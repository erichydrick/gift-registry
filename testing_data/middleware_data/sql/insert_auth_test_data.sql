INSERT INTO people (person_id, external_id, email, display_name, first_name, last_name, `type`) 
    VALUES (1, 'unprotected-endpoint-user', 'unprotectedEndpointTest@localhost.com', 'Unprotected', 'Unprotected', 'Endpoint', 'NORMAL'),
    (2, 'protected-endpoint-user', 'protectedEndpointTest@localhost.com', 'Protected', 'Protected', 'Endpoint', 'NORMAL'),
    (3, 'expired-session-user', 'expiredSessionTest@localhost.com', 'Expired', 'Expired', 'Session', 'NORMAL'),
    (4, 'wrong-agent-user', 'wrongUserAgentTest@localhost.com', 'Wrong', 'Wrong', 'Agent', 'NORMAL'), 
    (5, 'protected-endpoint-user-with-household', 'protectedEndpointWithHouseholdTest@localhost.com', 'With', 'With', 'Household', 'NORMAL');

INSERT INTO households (household_id, external_id, name)
    VALUES (1, 'protected-endpoint-household', 'Protected With House'),
        (2, 'middleware-rejected-household', 'Middleware Rejection');

INSERT INTO household_people (household_id, person_id)
    VALUES (1, 2),
        (1, 5),
        (2, 1), 
        (2, 3),
        (2, 4);

INSERT INTO sessions (session_id, person_id, expiration, user_agent)
    VALUES ('protected-endpoint-access', 2, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('expired-session-token', 3, Datetime('now', '-1 minutes'), 'test-user-agent'),
        ('wrong-agent-token', 3, Datetime('now', '+5 minutes'), 'test-user-agent'),
        ('protected-endpoint-user-with-household-access', 5, Datetime('now', '+5 minutes'), 'test-user-agent');
