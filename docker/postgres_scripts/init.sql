CREATE DATABASE gift_registry WITH ENCODING='UTF8' LC_CTYPE='en_US.utf8' LC_COLLATE='en_US.utf8';
\c gift_registry;

/* TODO: REFACTOR THIS ONCE I HAVE A PROPER STARTING SCHEMA */
CREATE TABLE IF NOT EXISTS person (
    id serial PRIMARY KEY,
    external_id varchar(50) UNIQUE NOT NULL,
    first_name varchar(255) NOT NULL,
    last_name varchar(255) NOT NULL,
    email_address varchar(255) NOT NULL
);
COMMIT;

/* 
    TODO: DELETE THIS ONCE I ESTABLISH THE APP LAUNCHES, CAN CONNECT TO THE DB, 
        AND START BUILDING THE WEB INTERFACE 
*/
INSERT INTO gift_registry.person (external_id, first_name, last_name, email_address) VALUES ('Eric', 'Hydrick', 'erichydrick@yopmail.com');
INSERT INTO gift_registry.person (external_id, first_name, last_name, email_address) VALUES ('Other', 'Hydrick', 'otherhydrick@yopmail.com');
