CREATE TABLE IF NOT EXISTS person (
    person_id SERIAL PRIMARY KEY, 
    email VARCHAR(255) NOT NULL UNIQUE,
    external_id CHAR(40) UNIQUE NOT NULL 
        CONSTRAINT ext_id_not_empty CHECK (TRIM(BOTH FROM external_id) <> ''),
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    display_name VARCHAR(255) NOT NULL
);
