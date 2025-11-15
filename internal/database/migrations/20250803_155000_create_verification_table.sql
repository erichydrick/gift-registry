CREATE TABLE IF NOT EXISTS verification (
    person_id INTEGER PRIMARY KEY REFERENCES person (person_id), 
    token VARCHAR(255) NOT NULL, 
    token_expiration TIMESTAMP WITH TIME ZONE, 
    attempts SMALLINT DEFAULT 0
);
