CREATE TABLE IF NOT EXISTS household (
    household_id SERIAL PRIMARY KEY,
    external_id VARCHAR(40) UNIQUE NOT NULL 
        CONSTRAINT ext_id_not_empty CHECK (TRIM(BOTH FROM external_id) <> ''),
    name VARCHAR(255) UNIQUE NOT NULL
        CONSTRAINT name_not_empty CHECK (TRIM(BOTH FROM name) <> '')
);
CREATE TABLE IF NOT EXISTS household_person (
    household_id INTEGER REFERENCES household (household_id),
    person_id INTEGER PRIMARY KEY UNIQUE REFERENCES person (person_id),
    CONSTRAINT one_household_per_person UNIQUE(household_id, person_id)
);
CREATE INDEX IF NOT EXISTS household_id on household_person (household_id);
