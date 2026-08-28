CREATE TABLE IF NOT EXISTS people (
    person_id INTEGER PRIMARY KEY AUTOINCREMENT, 
    email VARCHAR(255) NOT NULL,
    external_id VARCHAR(40) UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    first_name VARCHAR(255) NOT NULL CHECK (TRIM(first_name) <> ''),
    last_name VARCHAR(255) NOT NULL CHECK (TRIM(last_name) <> ''),
    display_name VARCHAR(255) NOT NULL, 
    type VARCHAR(100) NOT NULL DEFAULT 'NORMAL' CHECK (TRIM(type) <> '')
);
CREATE UNIQUE INDEX unique_email_values ON people (email) WHERE (email <> '');

CREATE TABLE IF NOT EXISTS verifications (
    person_id INTEGER PRIMARY KEY REFERENCES person (person_id), 
    token VARCHAR(255) NOT NULL, 
    token_expiration TIMESTAMP WITH TIME ZONE, 
    attempts SMALLINT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
    session_id VARCHAR(255) PRIMARY KEY NOT NULL, 
    person_id INTEGER REFERENCES person (person_id), 
    expiration TIMESTAMP NOT NULL, 
    user_agent BPCHAR
);
