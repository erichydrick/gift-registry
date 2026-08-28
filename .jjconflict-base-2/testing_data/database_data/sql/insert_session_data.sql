INSERT INTO people (person_id, external_id, email, display_name, first_name, last_name, `type`) 
    VALUES (1, 'not-expired-tokens', 'notexpiredtokens@localhost.com', 'Not', 'Not', 'Expired', 'NORMAL'),
        (2, 'expired-tokens', 'expiredtokens@localhost.com', 'Yes', 'Yes', 'Expired', 'NORMAL');

INSERT INTO sessions (session_id, person_id, expiration, user_agent) 
    VALUES ('not-expired-tokens', 1, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('expired-tokens', 1, Datetime('now', '-2 minutes'), 'test-user-agent');

INSERT INTO verifications (person_id, token, token_expiration, attempts) 
    VALUES (1, 'not-expired-tokens', Datetime('now', '+5 minutes'), 0),
        (2, 'expired-tokens', Datetime('now', '-2 minutes'), 0);
