-- Dev sign-in: dev@nodera.local / Nodera1234! (argon2id), verified, email MFA on.
INSERT INTO users (id, email, password, full_name, locale, country, status, mfa_method, email_verified_at)
VALUES (
    '019a0000-0000-7000-8000-000000000001',
    'dev@nodera.local',
    '$argon2id$v=19$m=65536,t=1,p=4$qa8sYQvS16KqHEDgxE9lVw$Fx9W4O9nejpcXm+M953RMqgLIrADpinv0m2h6tk7KR4',
    'Nodera Dev',
    'en',
    'TR',
    'active',
    'email',
    now()
);

INSERT INTO teams (id, name, owner_user_id)
VALUES ('019a0000-0000-7000-8000-000000000101', 'Nodera Dev', '019a0000-0000-7000-8000-000000000001');

INSERT INTO team_members (team_id, user_id, role)
VALUES ('019a0000-0000-7000-8000-000000000101', '019a0000-0000-7000-8000-000000000001', 'owner');
