CREATE TABLE IF NOT EXISTS session (
    session_id VARCHAR(255) PRIMARY KEY NOT NULL, 
    person_id INTEGER REFERENCES person (person_id), 
    expiration TIMESTAMP NOT NULL, 
    user_agent BPCHAR
);
