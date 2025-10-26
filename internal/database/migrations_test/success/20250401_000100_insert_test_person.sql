BEGIN TRANSACTION;
INSERT INTO person (email, first_name, last_name) VALUES ('test.user@yopmail.com', 'Test', 'User');
COMMIT TRANSACTION;

