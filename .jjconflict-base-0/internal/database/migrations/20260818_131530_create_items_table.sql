CREATE TABLE IF NOT EXISTS items (
    item_id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER REFERENCES person (person_id),
    external_id VARCHAR 40 UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    name VARCHAR(255) NOT NULL CHECK (TRIM(name) <> ''),
    url TEXT,
    notes TEXT,
    quantity INTEGER DEFAULT 1,
    gift_date DATE NOT NULL
);

CREATE TABLE IF NOT EXISTS item_claims (
    item_id INTEGER REFERENCES item (item_id),
    household_id INTEGER REFERENCES (household_id),
    CONSTRAINT households_claim_once UNIQUE(item_id, household_id)
);
