CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(250) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'user'
);