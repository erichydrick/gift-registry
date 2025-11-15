CREATE TABLE IF NOT EXISTS household_person (
    household_id INTEGER REFERENCES household (household_id),
    person_id INTEGER PRIMARY KEY REFERENCES person (person_id),
    CONSTRAINT one_household_per_person UNIQUE(household_id, person_id)
);
