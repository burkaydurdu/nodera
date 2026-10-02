-- The usage amount within a Period that triggers an early Invoice: lower for a
-- Team in its first months. Operators change the amounts by SQL.
CREATE TABLE thresholds (
    kind   TEXT PRIMARY KEY CHECK (kind IN ('new', 'established')),
    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0)
);
