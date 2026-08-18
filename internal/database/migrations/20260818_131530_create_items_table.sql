CREATE TABLE IF NOT EXISTS items (
    item_id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER REFERENCES person (person_id),
    external_id VARCHAR 40 UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    name VARCHAR(255) NOT NULL CHECK (TRIM(name) <> ''),
);
