-- The two newest minor versions at launch (docs/adr/0013); the engine's
-- ceiling was v1.37.0 when researched on 2026-10-09. Closing a version is an
-- UPDATE of offered, never a deploy.
INSERT INTO kubernetes_versions (version, offered) VALUES
    ('1.37.0', true),
    ('1.36.4', true)
ON CONFLICT (version) DO NOTHING;
