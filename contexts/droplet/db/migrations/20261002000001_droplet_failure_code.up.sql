-- Why a Droplet failed, as a code the web app and the emails word in the
-- reader's language; failure_reason keeps the English sentence for the
-- history and the logs. Earlier failures are coded from that sentence.
ALTER TABLE droplets ADD COLUMN failure_code TEXT NULL;

UPDATE droplets SET failure_code = CASE
    WHEN failure_reason = 'the server request was refused' THEN 'rejected'
    WHEN failure_reason = 'the server was not delivered in time' THEN 'timed_out'
    WHEN failure_reason = 'server no longer exists at the provider' THEN 'server_gone'
    WHEN failure_reason = 'provider reported failed' THEN 'provider_failed'
    ELSE 'unavailable'
END
WHERE failure_reason IS NOT NULL;
