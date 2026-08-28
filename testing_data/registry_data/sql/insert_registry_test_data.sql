INSERT INTO people (person_id, external_id, email, display_name, first_name, last_name, `type`)
    VALUES (1, 'person-one', 'mom_persona@localhost.com', 'Mom', 'Wife', 'TestFamily', 'NORMAL'),
        (2, 'person-two', 'dad_persona@localhost.com', 'Dad', 'Husband', 'TestFamily', 'NORMAL'),
        (3, 'person-three', NULL, 'Son', 'Son', 'TestFamily', 'MANAGED'),
        (4, 'person-four', NULL, 'Daughter', 'Daughter', 'TestFamily', 'MANAGED'),
        (5, 'person-five', 'grandpa_persona@localhost.com', 'Grandpa', 'Grandfather', 'Grandtester', 'NORMAL'),
        (6, 'person-six', 'grandma_persona@localhost.com', 'Grandma', 'Grandmother', 'Grandtester', 'NORMAL'),
        (7, 'person-seven', 'aunt_persona@localhost.com', 'Aunt', 'Sister', 'OtherFamily', 'NORMAL'),
        (8, 'person-eight', 'uncle_persona@localhost.com', 'Uncle', 'BiL', 'OtherFamily', 'NORMAL')
;

INSERT INTO households (household_id, external_id, name)
    VALUES (1, 'house-one', 'The Testers'),
        (2, 'house-two', 'Grandma & Grandpa'), 
        (3, 'house-three', 'The OtherFamily') 
;

INSERT INTO household_people (household_id, person_id)
    VALUES (1, 1),
        (1, 2),
        (1, 3),
        (1, 4),
        (2, 5),
        (2, 6),
        (3, 7), 
        (3, 8)
;

INSERT INTO sessions (session_id, person_id, expiration, user_agent)
    VALUES ('mom-registry-session', 1, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('grandma-registry-session', 6, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('dad-registry-session', 2, Datetime('now', '+5 minutes'), 'test-user-agent')
;

INSERT INTO items (item_id, gift_for, added_by, external_id, name, url, notes, quantity)
    VALUES (1, 3, 1, 'gift-1', 'New bike', NULL, 'He outgrew his last one', 1),
        (2, 3, 1, 'gift-2', 'New helmet', 'https://www.walmart.com', NULL, 1),
        (3, 4, 1, 'gift-3', 'Headbands', 'https://www.amazon.com', 'She can never have too many', 10),
        (4, 4, 1, 'gift-4', 'Doll', NULL, NULL, 1),
        (5, 1, 1, 'gift-5', 'KitchenAid Attachments', NULL, 'Have the pasta one', 3)
;

INSERT INTO item_claims (item_id, household_id, claimed_by, claim_type, gift_date, quantity)
    VALUES (1, 2, 5, 'JOINT', Date('now', '+14 days'), 1),
        (1, 3, 7, 'JOINT', Date('now', '+14 days'), 0),
        (3, 2, 6, 'PARTIAL', Date('now', '+14 days'), 4),
        (5, 3, 8, 'FULL', Date('now', '+14 days'), 1)
;
