-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      text NOT NULL,
    username_key  text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    balance       bigint NOT NULL CHECK (balance >= 0),
    is_admin      boolean NOT NULL DEFAULT false,
    bonus_at      timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Rooms of the first schema are anonymous: they have no owner and their
-- players no accounts to return chips to. Deleting them also deletes their
-- players and hand history.
DELETE FROM rooms;

ALTER TABLE rooms
    DROP COLUMN stack,
    ADD COLUMN owner_id   bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ADD COLUMN public     boolean NOT NULL,
    ADD COLUMN min_buy_in integer NOT NULL,
    ADD COLUMN max_buy_in integer NOT NULL,
    ADD CONSTRAINT rooms_buy_in_check CHECK (big_blind <= min_buy_in AND min_buy_in <= max_buy_in);
CREATE INDEX rooms_owner_id_idx ON rooms (owner_id);

ALTER TABLE players
    DROP COLUMN token_hash,
    ADD COLUMN user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ADD CONSTRAINT players_room_code_user_id_key UNIQUE (room_code, user_id);
CREATE INDEX players_user_id_idx ON players (user_id);

ALTER TABLE hand_seats
    ADD COLUMN user_id bigint REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX hand_seats_user_id_idx ON hand_seats (user_id);

-- +goose Down
DELETE FROM rooms;

DROP INDEX hand_seats_user_id_idx;
ALTER TABLE hand_seats DROP COLUMN user_id;

DROP INDEX players_user_id_idx;
ALTER TABLE players
    DROP CONSTRAINT players_room_code_user_id_key,
    DROP COLUMN user_id,
    ADD COLUMN token_hash bytea NOT NULL;

DROP INDEX rooms_owner_id_idx;
ALTER TABLE rooms
    DROP CONSTRAINT rooms_buy_in_check,
    DROP COLUMN owner_id,
    DROP COLUMN public,
    DROP COLUMN min_buy_in,
    DROP COLUMN max_buy_in,
    ADD COLUMN stack integer NOT NULL CHECK (stack > 0);

DROP TABLE sessions;
DROP TABLE users;
