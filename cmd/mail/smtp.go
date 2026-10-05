package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/contexts/mail/smtp"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/str"
)

// RunSMTP serves the relay on three ports. Credentials never travel in clear:
// AUTH is refused before STARTTLS on the submission and alternative ports.
func RunSMTP(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, newRabbitMQClient(ctx, conf, log), log)
	if err != nil {
		log.Fatal(err)
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-mail-smtp", outboxInstance)
	defer pool.Close()

	tlsConfig, err := relayTLS(conf)
	if err != nil {
		log.Fatal(err)
	}
	backend := app.BuildSMTPBackend(deps, app.NewHandlers(deps))

	serve := func(addr string, implicitTLS bool) {
		server := gosmtp.NewServer(backend)
		smtp.Configure(server, conf.SMTP.Hostname, conf.SMTP.MaxMessageBytes)
		server.Addr = addr
		server.TLSConfig = tlsConfig
		if implicitTLS {
			log.Fatal(server.ListenAndServeTLS())
		}
		log.Fatal(server.ListenAndServe())
	}

	go serve(conf.SMTP.TLSAddr, true)
	go serve(conf.SMTP.AltAddr, false)
	serve(conf.SMTP.SubmissionAddr, false)
}

// relayTLS loads the relay's certificate; development without one gets a
// throwaway self-signed certificate so STARTTLS still works.
func relayTLS(conf *config.Config) (*tls.Config, error) {
	if str.IsNotEmpty(conf.SMTP.CertFile) {
		cert, err := tls.LoadX509KeyPair(conf.SMTP.CertFile, conf.SMTP.KeyFile)
		if err != nil {
			return nil, err
		}

		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	}
	if !conf.IsDev() {
		return nil, errNoCertificate
	}

	cert, err := SelfSigned(conf.SMTP.Hostname)
	if err != nil {
		return nil, err
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// SelfSigned is a certificate for hostname valid for a day.
func SelfSigned(hostname string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: hostname},
		DNSNames:     []string{hostname, "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
