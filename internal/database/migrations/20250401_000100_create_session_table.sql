CREATE TABLE IF NOT EXISTS sessions (
    session_id VARCHAR(255) PRIMARY KEY NOT NULL, 
    person_id INTEGER REFERENCES people (person_id), 
    expiration TIMESTAMP NOT NULL, 
    user_agent BPCHAR
);
