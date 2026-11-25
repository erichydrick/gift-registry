CREATE TABLE IF NOT EXISTS verifications (
    person_id INTEGER PRIMARY KEY REFERENCES people (person_id) ON DELETE CASCADE, 
    token VARCHAR(255) NOT NULL, 
    token_expiration TIMESTAMP, 
    attempts SMALLINT DEFAULT 0
);
