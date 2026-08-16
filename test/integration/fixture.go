package integration

const fixtureSQL = `
CREATE TABLE organizations (
  id   bigint PRIMARY KEY,
  name text NOT NULL
);
CREATE TABLE buyers (
  id     bigint PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES organizations(id),
  name   text NOT NULL
);
CREATE TABLE invoices (
  id            bigserial PRIMARY KEY,
  org_id        bigint NOT NULL REFERENCES organizations(id),
  buyer_id      bigint REFERENCES buyers(id),
  status        text NOT NULL,
  amount_total  numeric(14,2),
  currency      text,
  internal_note text,
  active        boolean NOT NULL DEFAULT true,
  deleted_at    timestamptz
);
CREATE ROLE plinth_ro LOGIN PASSWORD 'ro_pass';
GRANT USAGE ON SCHEMA public TO plinth_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO plinth_ro;
INSERT INTO organizations (id, name) VALUES (1, 'Org One'), (2, 'Org Two');
INSERT INTO invoices (org_id, status, amount_total, currency) VALUES
  (1, 'OPEN',   100.00, 'IDR'),
  (1, 'PAID',   250.00, 'IDR'),
  (2, 'OPEN',    50.00, 'USD');
`
