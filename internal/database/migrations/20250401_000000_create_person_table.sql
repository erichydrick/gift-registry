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
