CREATE TABLE IF NOT EXISTS person (
    person_id SERIAL PRIMARY KEY, 
    email VARCHAR(255) NOT NULL,
    external_id VARCHAR(40) UNIQUE NOT NULL,
      CONSTRAINT ext_id_not_empty CHECK (TRIM(BOTH FROM external_id) <> ''),
    first_name VARCHAR(255) NOT NULL,
       CONSTRAINT first_name_not_empty CHECK (TRIM(BOTH FROM first_name) <> ''),
    last_name VARCHAR(255) NOT NULL,
       CONSTRAINT last_name_not_empty CHECK (TRIM(BOTH FROM last_name) <> ''),
    display_name VARCHAR(255) NOT NULL, 
    type VARCHAR(100) NOT NULL DEFAULT 'NORMAL',
        CONSTRAINT type_not_empty CHECK (TRIM(BOTH FROM type) <> '')
);
CREATE UNIQUE INDEX unique_email_values ON person (email) WHERE (email <> '');

CREATE TABLE IF NOT EXISTS verification (
    person_id INTEGER PRIMARY KEY REFERENCES person (person_id), 
    token VARCHAR(255) NOT NULL, 
    token_expiration TIMESTAMP WITH TIME ZONE, 
    attempts SMALLINT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS session (
    session_id VARCHAR(255) PRIMARY KEY NOT NULL, 
    person_id INTEGER REFERENCES person (person_id), 
    expiration TIMESTAMP NOT NULL, 
    user_agent BPCHAR
);
