-- KumoMTA policy for Nodera Mail (docs/adr/0009, docs/adr/0011).
--
-- The mail service is the source of truth: it injects every Message over
-- HTTP with its Project as the tenant, hands out each Sending domain's
-- signing key, and receives every outcome through the log hook. Nothing here
-- is configured per Team.
--
-- Environment:
--   NODERA_ENV                 dev | test | alpha | prod
--   MAIL_INTERNAL_URL          the mail service's internal listener
--   MAIL_INTERNAL_TOKEN        the bearer token that listener accepts
--   KUMO_INJECT_USER           the Basic credentials the mail service
--   KUMO_INJECT_PASSWORD         injects with
--   KUMO_HOSTNAME              this MTA's name, for EHLO and the inbound listener
--   KUMO_TRUSTED_HOSTS         comma-separated addresses allowed the admin API
--   KUMO_SMART_HOST            development only: every Message goes to this host:port
--   KUMO_SOURCES               outside development: "name=ip,..." of the shared egress pool
--   MAIL_ZONE                  the zone of docs/adr/0011, whose rp. name takes feedback loops

local kumo = require 'kumo'
local log_hooks = require 'policy-extras.log_hooks'
local shaping = require 'policy-extras.shaping'

local function list(value)
  local out = {}
  for item in string.gmatch(value or '', '[^,]+') do
    local trimmed = item:gsub('^%s+', ''):gsub('%s+$', '')
    table.insert(out, trimmed)
  end
  return out
end

local function sources(value)
  local out = {}
  for _, pair in ipairs(list(value)) do
    local name, ip = pair:match('^([^=]+)=(.+)$')
    if name then
      out[name] = ip
    end
  end
  return out
end

local MAIL_URL = os.getenv 'MAIL_INTERNAL_URL'
local MAIL_TOKEN = os.getenv 'MAIL_INTERNAL_TOKEN'
local INJECT_USER = os.getenv 'KUMO_INJECT_USER' or 'mail-service'
local INJECT_PASSWORD = os.getenv 'KUMO_INJECT_PASSWORD'
local HOSTNAME = os.getenv 'KUMO_HOSTNAME' or 'localhost'
local SMART_HOST = os.getenv 'KUMO_SMART_HOST'
local SOURCES = sources(os.getenv 'KUMO_SOURCES')
local ZONE = os.getenv 'MAIL_ZONE' or 'mta.nodera.local'

-- The log hook must be registered before any catch-all get_queue_config.
-- A record the mail service answers 4xx to can never be read and is dropped;
-- anything else is retried, and the service applies a record only once.
log_hooks:new {
  name = 'mailsvc',
  log_parameters = { meta = { 'tenant', 'x_message_id', 'x_position' } },
  queue_config = { retry_interval = '30s', max_retry_interval = '10m', max_age = '3 days' },
  -- true drops the record: the mail service records acceptance itself. The
  -- filter, not per_record, drops Reception, which would otherwise take the
  -- asynchronous bounce records made in the same step with it.
  filter = function(msg, hook_name)
    local kind = msg:get_meta('log_record').type
    return kind == 'Reception' or kind == 'Delayed'
  end,
  constructor = function(domain, tenant, campaign)
    local connection = {}
    local client = kumo.http.build_client { timeout = '10s' }

    function connection:send(message)
      local response = client
        :post(MAIL_URL .. '/internal/v1/mta/events')
        :header('Content-Type', 'application/json')
        :bearer_auth(MAIL_TOKEN)
        :body(message:get_data())
        :send()
      local disposition = string.format('%d %s', response:status_code(), response:text())
      if response:status_is_success() then
        return disposition
      end
      if response:status_is_client_error() then
        kumo.reject(500, disposition)
      end
      kumo.reject(421, disposition)
    end

    function connection:close()
      client:close()
    end

    return connection
  end,
}

local shaper = shaping:setup_with_automation {
  extra_files = { '/opt/kumomta/share/community/shaping.toml' },
}

-- A Sending domain's active key, cached so a burst of Messages asks the mail
-- service once; a rotation reaches the MTA within the cache's lifetime while
-- the previous key stays published.
local function fetch_signing_key(domain_id)
  local response = kumo.http.build_client({ timeout = '5s' })
    :get(MAIL_URL .. '/internal/v1/dkim-keys/' .. domain_id)
    :bearer_auth(MAIL_TOKEN)
    :send()
  if response:status_code() == 404 then
    return nil
  end
  if not response:status_is_success() then
    error(string.format('signing key %s: %d %s', domain_id, response:status_code(), response:text()))
  end
  return kumo.serde.json_parse(response:text())
end

local signing_key = kumo.memoize(fetch_signing_key, {
  name = 'mail_signing_keys',
  ttl = '5 minutes',
  capacity = 10000,
})

kumo.on('init', function()
  kumo.define_spool { name = 'data', path = '/var/spool/kumomta/data', kind = 'RocksDB' }
  kumo.define_spool { name = 'meta', path = '/var/spool/kumomta/meta', kind = 'RocksDB' }
  kumo.configure_local_logs { log_dir = '/var/log/kumomta', max_segment_duration = '10s' }
  kumo.configure_bounce_classifier { files = { '/opt/kumomta/share/bounce_classifier/iana.toml' } }

  -- A Message may be 10 MB decoded; its base64 and the JSON it is posted in
  -- make it larger than KumoMTA's 2 MB default, which would answer 413.
  kumo.start_http_listener {
    listen = '0.0.0.0:8000',
    trusted_hosts = list(os.getenv 'KUMO_TRUSTED_HOSTS' or '127.0.0.1,::1'),
    request_body_limit = 32 * 1024 * 1024,
  }
  -- Inbound only: asynchronous bounces and feedback-loop reports. Nothing is
  -- relayed from it.
  kumo.start_esmtp_listener {
    listen = '0.0.0.0:25',
    hostname = HOSTNAME,
    relay_hosts = { '127.0.0.1' },
  }
end)

kumo.on('http_server_validate_auth_basic', function(user, password)
  return INJECT_PASSWORD ~= nil and INJECT_PASSWORD ~= '' and user == INJECT_USER and password == INJECT_PASSWORD
end)

-- Every injected copy carries the mail service's metadata; signing is the
-- last step, so nothing changes the copy after it. Each recipient's copy goes
-- out with its own return path, which names the recipient to an asynchronous
-- bounce (VERP); the trace header carries it for feedback-loop reports.
kumo.on('http_message_generated', function(msg, auth_info)
  local extra = msg:get_meta 'extra' or {}
  if not extra.message_id or not extra.position or not extra.return_path or not extra.sending_domain_id or not extra.tenant then
    kumo.reject(550, 'injected without the mail service metadata')
  end

  msg:set_sender(tostring(extra.return_path))
  msg:set_meta('tenant', tostring(extra.tenant))
  msg:set_meta('x_message_id', tostring(extra.message_id))
  msg:set_meta('x_position', tostring(extra.position))
  msg:set_meta('x_return_path', tostring(extra.return_path))

  local key = signing_key(tostring(extra.sending_domain_id))
  if not key then
    kumo.reject(550, 'no dkim key for the sending domain')
  end

  msg:dkim_sign(kumo.dkim.rsa_sha256_signer {
    domain = key.domain,
    selector = key.selector,
    headers = { 'From', 'To', 'Cc', 'Subject', 'Date', 'Message-ID', 'Reply-To', 'MIME-Version', 'Content-Type', 'Feedback-ID' },
    key = { key_data = key.private_key_pem },
    over_sign = true,
    ttl = '5 minutes',
  })
end)

-- A copy's return path is at n<token>.<domain>, reaching this listener
-- through the Team's CNAME; rp.<zone> takes feedback-loop reports. Both are
-- logged for the mail service and dropped, never relayed: relaying a bounce
-- to the return path would loop back here.
kumo.on('get_listener_domain', function(domain, listener, conn_meta)
  if domain:match('^n%w%w%w%w%w%w%w%w%.') or domain == 'rp.' .. ZONE then
    return kumo.make_listener_domain { log_oob = 'LogThenDrop', log_arf = 'LogThenDrop' }
  end
end)

kumo.on('get_queue_config', function(domain, tenant, campaign, routing_domain)
  if SMART_HOST then
    return kumo.make_queue_config { protocol = { smtp = { mx_list = { SMART_HOST } } } }
  end
  return kumo.make_queue_config { egress_pool = 'shared' }
end)

kumo.on('get_egress_source', function(name)
  return kumo.make_egress_source { name = name, source_address = SOURCES[name], ehlo_domain = HOSTNAME }
end)

kumo.on('get_egress_pool', function(name)
  local entries = {}
  for source in pairs(SOURCES) do
    table.insert(entries, { name = source })
  end
  return kumo.make_egress_pool { name = name, entries = entries }
end)

-- The development smart host speaks plain SMTP and may sit on a private
-- address; real destinations follow the community traffic-shaping rules.
kumo.on('get_egress_path_config', function(domain, source, site)
  if SMART_HOST then
    return kumo.make_egress_path { enable_tls = 'Disabled', prohibited_hosts = {} }
  end
  return shaper.get_egress_path_config(domain, source, site)
end)
