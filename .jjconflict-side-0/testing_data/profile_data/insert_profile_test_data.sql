INSERT INTO person (person_id, external_id, email, display_name, first_name, last_name, `type`) 
    VALUES (1, 'succ-disp-name', 'displayName@localhost.com', 'Root', 'Display', 'Named', 'NORMAL'),
        (2, 'succ-def-disp-name', 'nodisplayname@localhost.com', 'Display', 'Display', 'Nameless', 'NORMAL'),
        (3, 'manager-profile', 'profilewithkids@localhost.com', 'Root', 'Display', 'Named', 'NORMAL'),
        (4, 'child-1-profile', '', 'Junior', 'Firstborn', 'Named', 'MANAGED'),
        (5, 'child-2-profile', '', 'Baby', 'Secondborn', 'Named', 'MANAGED'),
        (6, 'profile-load-bad-temp', 'getprofilebadtemplate@localhost.com', 'Get', 'Get', 'Profile', 'NORMAL'),
        (7, 'profile-update-bad-temp', 'updateprofilebadtemplate@localhost.com', 'Update', 'Update', 'Profile', 'NORMAL'),
        (8, 'success-update', 'completedupdate@localhost.com', 'Sudo', 'Completed', 'Modification', 'NORMAL'),
        (9, 'bad-first-name', 'failedupdatenofirstname@localhost.com', 'Root', 'Nofirst', 'Name', 'NORMAL'),
        (10, 'bad-last-email', 'failedupdatemultipleFields@localhost.com', 'Root', 'FailedLastAndEmail', 'Update', 'NORMAL'),
        (11, 'clear-display', 'cleardisplayname@localhost.com', 'Blanked', 'Clear', 'Displayname', 'NORMAL'),
        (12, 'update-manager-profile', 'managedprofileupdate@localhost.com', 'Root', 'Successful', 'Update', 'NORMAL'),
        (13, 'update-managed-profile-1', '', 'NotToBe', 'NotToBe', 'Modified', 'MANAGED'),
        (14, 'update-managed-profile-2', '', 'NotYet', 'HasBeen', 'Modified', 'MANAGED'),
        (15, 'valid-household', 'validhouseholdname@localhost.com', 'Valid', 'Valid', 'Household', 'NORMAL');

INSERT INTO household (household_id, external_id, name) 
    VALUES (1, 'disp-household', 'Disp'),
        (2, 'update-profile-existing-household', 'Existing Household Success'),
        (3, 'update-household-name-succ', 'Valid household');

INSERT INTO household_person (household_id, person_id) 
    VALUES (1, 1),
        (1, 2),
        (1, 3),
        (1, 4),
        (1, 5),
        (2, 8),
        (2, 9),
        (2, 10),
        (2, 11),
        (2, 12),
        (2, 13),
        (2, 14),
        (3, 15);

INSERT INTO session (session_id, person_id, expiration, user_agent) 
    VALUES ('succ-disp-name-token', 1, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('succ-def-disp-name-token', 2, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('manager-profile-token', 3, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('profile-load-bad-temp-token', 6, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('profile-update-bad-temp-token', 7, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('success-update-token', 8, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('bad-first-name-token', 9, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('bad-last-email-token', 10, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('clear-display-token', 11, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('update-manager-profile-token', 12, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('valid-household-token', 15, Datetime('now', '+5 minutes'), 'test-user-agent');
