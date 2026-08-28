CREATE TABLE IF NOT EXISTS items (
    item_id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (item_id > 0),
    gift_for INTEGER REFERENCES people (person_id),
    added_by INTEGER REFERENCES people (person_id),
    external_id VARCHAR(40) UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    name VARCHAR(255) NOT NULL CHECK (TRIM(name) <> ''),
    url TEXT,
    notes TEXT,
    quantity INTEGER DEFAULT 1,
    added_on DATE NOT NULL DEFAULT CURRENT_DATE,
    last_updated_on DATE NOT NULL DEFAULT CURRENT_DATE
);

CREATE TABLE IF NOT EXISTS item_claims (
    item_id INTEGER REFERENCES items (item_id),
    household_id INTEGER REFERENCES households (household_id),
    claimed_by INTEGER REFERENCES people (person_id),
    claimed_on DATE NOT NULL DEFAULT CURRENT_DATE,
    gift_date DATE NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(10) NOT NULL DEFAULT 'CLAIMED' CHECK (TRIM(status) <> ''),
    claim_type VARCHAR(10) NOT NULL DEFAULT 'FULL' CHECK (TRIM(claim_type) <> ''),
    CONSTRAINT households_claim_once UNIQUE(item_id, household_id)
);

