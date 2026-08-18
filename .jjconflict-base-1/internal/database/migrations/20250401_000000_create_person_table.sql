CREATE TABLE IF NOT EXISTS people (
    person_id INTEGER PRIMARY KEY AUTOINCREMENT, 
    email VARCHAR(255) NOT NULL,
    external_id VARCHAR(40) UNIQUE NOT NULL CHECK (TRIM(external_id) <> ''),
    first_name VARCHAR(255) NOT NULL CHECK (TRIM(first_name) <> ''),
    last_name VARCHAR(255) NOT NULL CHECK (TRIM(last_name) <> ''),
    display_name VARCHAR(255) NOT NULL,
    type VARCHAR(100) NOT NULL DEFAULT 'NORMAL' CHECK (TRIM(type) <> '')
);
CREATE UNIQUE INDEX unique_email_values ON people (email) WHERE (email <> '');
CREATE TRIGGER email_required_normal_profile_insert
    AFTER INSERT ON people 
    WHEN NEW.type = 'NORMAL' AND (NEW.email IS NULL OR TRIM(New.email) = '')
    BEGIN
        SELECT RAISE(ABORT, 'Email is required for NORMAL accounts');
    END;
CREATE TRIGGER email_required_normal_profile_update
    AFTER UPDATE ON people
    WHEN NEW.type = 'NORMAL' AND (NEW.email IS NULL OR TRIM(New.email) = '')
    BEGIN
        SELECT RAISE(ABORT, 'Email is required for NORMAL accounts');
    END;
