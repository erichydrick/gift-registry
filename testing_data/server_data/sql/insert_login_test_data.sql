INSERT INTO person (person_id, external_id, email, display_name, first_name, last_name, `type`) 
    VALUES (1, 'registered-user', 'registeredUser@localhost.com', 'Allgood', 'Valid', 'Email', 'NORMAL'),
        (2, 'unregistered-user', 'unregisteredUser@localhost.com', 'Whoami', 'Unregistered', 'User', 'NORMAL'),
        (3, 'expired-token-user', 'expiredTokenTest@localhost.com', 'Expired', 'Expired', 'Token', 'NORMAL'),
        (4, 'attempts-exceeeded-user', 'exceededAttemptsTokenTest@localhost.com', 'Exceeded', 'Exceeded', 'Attempts', 'NORMAL'),
        (5, 'attempts-limit-user', 'maxedFailuresTokenTest@localhost.com', 'Maxed', 'Maxed', 'Failures', 'NORMAL'),
        (6, 'wrong-token-user', 'moreTriesTokenTest@localhost.com', 'More', 'More', 'Tries', 'NORMAL'),
        (7, 'logout-user', 'testsuccessfullogout@localhost.com', 'Test', 'Test', 'User', 'NORMAL');

INSERT INTO verification (token, person_id, token_expiration, attempts) 
    VALUES ('registered-user-token', 1, Datetime('now', '+5 minutes'), 0),
        ('expired-token', 3, Datetime('now', '-5 minutes'), 0),
        ('attempts-exceeeded-user', 4, Datetime('now', '+5 minutes'), 1000),
        ('attempts-at-max-user', 5, Datetime('now', '+5 minutes'), 3),
        ('thisisright', 6, Datetime('now', '+5 minutes'), 0);


INSERT INTO session (session_id, person_id, expiration, user_agent) 
    VALUES ('logout-success-session', 7, Datetime('now', '+5 minutes'), 'test-user-agent');
