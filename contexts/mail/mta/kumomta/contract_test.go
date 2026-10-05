package kumomta_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/contexts/mail/mta/kumomta"
)

const (
	kumoImage   = "ghcr.io/kumocorp/kumomta"
	kumoTag     = "2026.09.29-b90d8bc1"
	contractTok = "contract-internal-token"
	contractPwd = "contract-inject-password"
	selector    = "n4k2x9q1za"
)

// sink is the SMTP server KumoMTA delivers to instead of a real MX.
type sink struct {
	mu        sync.Mutex
	delivered []delivered
}

// delivered is one copy as the receiving server saw it.
type delivered struct {
	sender    string
	recipient string
	data      []byte
}

func (s *sink) NewSession(*gosmtp.Conn) (gosmtp.Session, error) { return &sinkSession{sink: s}, nil }

type sinkSession struct {
	sink      *sink
	sender    string
	recipient string
}

func (ss *sinkSession) Mail(from string, _ *gosmtp.MailOptions) error {
	ss.sender = from

	return nil
}

func (ss *sinkSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	ss.recipient = to

	return nil
}

func (*sinkSession) Reset()        {}
func (*sinkSession) Logout() error { return nil }

func (ss *sinkSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	ss.sink.mu.Lock()
	ss.sink.delivered = append(ss.sink.delivered, delivered{sender: ss.sender, recipient: ss.recipient, data: raw})
	ss.sink.mu.Unlock()

	return err
}

func (s *sink) received() []delivered {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]delivered(nil), s.delivered...)
}

func (s *sink) to(recipient string) (delivered, bool) {
	for _, d := range s.received() {
		if d.recipient == recipient {
			return d, true
		}
	}

	return delivered{}, false
}

// The contract runs the repository's policy in a real KumoMTA: the adapter
// injects, the policy signs with the key the mail service hands out, a sink
// receives the signed Message, and the log hook reports the delivery back. It
// pulls the KumoMTA image, so it runs only when asked to.
func TestKumoMTAContract(t *testing.T) {
	if os.Getenv("NODERA_MAIL_SMOKE") == "" {
		t.Skip("set NODERA_MAIL_SMOKE=true to run the policy in a real KumoMTA")
	}

	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	require.NoError(t, err)
	domainID := uuid.New()

	var mu sync.Mutex
	var records [][]byte
	internal := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+contractTok {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		switch {
		case r.URL.Path == "/internal/v1/dkim-keys/"+domainID.String():
			_ = json.NewEncoder(w).Encode(map[string]string{
				"domain": "example.com", "selector": selector,
				"private_key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
			})
		case r.URL.Path == "/internal/v1/mta/events":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			records = append(records, body)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	internalListener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	go func() { _ = internal.Serve(internalListener) }()
	t.Cleanup(func() { _ = internal.Close() })

	mx := &sink{}
	smtpServer := gosmtp.NewServer(mx)
	smtpServer.Domain = "sink.test"
	sinkListener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	go func() { _ = smtpServer.Serve(sinkListener) }()
	t.Cleanup(func() { _ = smtpServer.Close() })

	policy, err := filepath.Abs("../../../../deploy/kumomta")
	require.NoError(t, err)

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pool.MaxWait = 3 * time.Minute
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: kumoImage, Tag: kumoTag,
		Env: []string{
			"NODERA_ENV=test",
			fmt.Sprintf("MAIL_INTERNAL_URL=http://host.docker.internal:%d", internalListener.Addr().(*net.TCPAddr).Port),
			"MAIL_INTERNAL_TOKEN=" + contractTok,
			"KUMO_INJECT_USER=mail-service",
			"KUMO_INJECT_PASSWORD=" + contractPwd,
			"KUMO_HOSTNAME=mx.contract.test",
			fmt.Sprintf("KUMO_SMART_HOST=host.docker.internal:%d", sinkListener.Addr().(*net.TCPAddr).Port),
		},
		Mounts:       []string{policy + ":/opt/kumomta/etc/policy:ro"},
		ExtraHosts:   []string{"host.docker.internal:host-gateway"},
		ExposedPorts: []string{"8000/tcp"},
	}, func(hostConfig *docker.HostConfig) {
		hostConfig.AutoRemove = true
		hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Purge(resource) })

	injectURL := "http://127.0.0.1:" + resource.GetPort("8000/tcp") + "/api/inject/v1"
	require.NoError(t, pool.Retry(func() error {
		res, err := http.Get(strings.Replace(injectURL, "/api/inject/v1", "/api/check-liveness/v1", 1))
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("liveness %d", res.StatusCode)
		}

		return nil
	}))

	paths := message.NewReturnPaths([]byte("contract-return-path-key"))
	host := "n4k2x9q1z.example.com"
	messageID := uuid.New()
	// Past KumoMTA's default 2 MB request limit once base64 and JSON grow it.
	attachment := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("nodera"), 512*1024))
	content := "From: Shop <no-reply@example.com>\r\nTo: ana@example.net, bob@example.net\r\nSubject: Contract\r\n" +
		"Date: Mon, 12 Oct 2026 10:00:00 +0000\r\nMessage-ID: <" + messageID.String() + "@example.com>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nSigned by the policy.\r\n" +
		"--b\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\n\r\n" + wrap(attachment) + "--b--\r\n"
	client := kumomta.New(kumomta.Config{InjectionURL: injectURL, Username: "mail-service", Password: contractPwd})
	result, err := client.Inject(context.Background(), &mta.Injection{
		MessageID: messageID,
		Content:   []byte(content),
		Envelope: message.Envelope{
			ProjectID: uuid.New(), TeamID: uuid.New(), SendingDomainID: domainID, DKIMDomain: "example.com",
			Recipients: []message.EnvelopeRecipient{
				{Email: "ana@example.net", Position: 0, ReturnPath: paths.For(messageID, 0, host)},
				{Email: "bob@example.net", Position: 1, ReturnPath: paths.For(messageID, 1, host)},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, result.Failed)

	require.Eventually(t, func() bool { return len(mx.received()) == 2 }, time.Minute, 500*time.Millisecond, "the sink never received both copies")
	for position, recipient := range []string{"ana@example.net", "bob@example.net"} {
		copyOf, ok := mx.to(recipient)
		require.True(t, ok, recipient)
		assert.Equal(t, paths.For(messageID, position, host), copyOf.sender, "each copy goes out with its own return path")

		verifications, err := dkim.VerifyWithOptions(bytes.NewReader(copyOf.data), &dkim.VerifyOptions{
			LookupTXT: func(domain string) ([]string, error) {
				if domain != selector+"._domainkey.example.com" {
					return nil, fmt.Errorf("unexpected lookup %s", domain)
				}

				return []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(public)}, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, verifications, 1)
		require.NoError(t, verifications[0].Err, "the policy signed with the Sending domain's key")
		require.Equal(t, "example.com", verifications[0].Domain)
	}

	parser := kumomta.NewRecords(paths)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		delivered := map[int]bool{}
		for _, r := range records {
			outcomes, err := parser.Parse(r)
			if err != nil {
				continue
			}
			for _, o := range outcomes {
				if o.MessageID == messageID && o.Type == message.EventDelivered {
					delivered[o.Position] = true
				}
			}
		}

		return delivered[0] && delivered[1]
	}, time.Minute, 500*time.Millisecond, "the log hook never reported both deliveries by position")
}

// wrap breaks base64 into the 76-character lines MIME allows.
func wrap(encoded string) string {
	var b strings.Builder
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")

	return b.String()
}
