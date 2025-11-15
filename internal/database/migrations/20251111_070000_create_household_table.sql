CREATE TABLE IF NOT EXISTS household (
    household_id SERIAL PRIMARY KEY,
    external_id CHAR(40) UNIQUE NOT NULL 
        CONSTRAINT ext_id_not_empty CHECK (TRIM(BOTH FROM external_id) <> ''),
    name VARCHAR(255) UNIQUE NOT NULL
        CONSTRAINT name_not_empty CHECK (TRIM(BOTH FROM name) <> '')
);
