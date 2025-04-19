/*
DROP DATABASE IF EXISTS gift_registry;
COMMIT;

CREATE DATABASE gift_registry WITH ENCODING='UTF8' LC_CTYPE='en_US.utf8' LC_COLLATE='en_US.utf8';
\c gift_registry;

-- TODO: REFACTOR THIS ONCE I HAVE A PROPER STARTING SCHEMA
CREATE TABLE IF NOT EXISTS settings (
    id serial PRIMARY KEY,
    name varchar(255) NOT NULL, 
    value varchar(255) NOT NULL
);
CREATE UNIQUE INDEX settings_name ON settings (name); 
COMMIT;
*/
