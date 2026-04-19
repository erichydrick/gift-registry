CREATE TABLE IF NOT EXISTS person (
    person_id INTEGER PRIMARY KEY AUTOINCREMENT, 
    email VARCHAR(255) NOT NULL,
    external_id VARCHAR(40) UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    first_name VARCHAR(255) NOT NULL CHECK (TRIM(first_name) <> ''),
    last_name VARCHAR(255) NOT NULL CHECK (TRIM(last_name) <> ''),
    display_name VARCHAR(255) NOT NULL,
    type VARCHAR(100) NOT NULL DEFAULT 'NORMAL' CHECK (TRIM(type) <> '')
);
CREATE UNIQUE INDEX unique_email_values ON person (email) WHERE (email <> '');
