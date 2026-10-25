-- +goose Up
CREATE TABLE rooms (
    code        text PRIMARY KEY,
    small_blind integer NOT NULL CHECK (small_blind > 0),
    big_blind   integer NOT NULL CHECK (big_blind >= small_blind),
    stack       integer NOT NULL CHECK (stack > 0),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE players (
    room_code  text NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
    seat       smallint NOT NULL CHECK (seat >= 0),
    name       text NOT NULL,
    token_hash bytea NOT NULL,
    stack      integer NOT NULL CHECK (stack >= 0),
    PRIMARY KEY (room_code, seat)
);

CREATE TABLE hands (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_code   text NOT NULL REFERENCES rooms (code) ON DELETE CASCADE,
    number      integer NOT NULL,
    board       text[] NOT NULL,
    pot         integer NOT NULL,
    finished_at timestamptz NOT NULL,
    UNIQUE (room_code, number)
);

CREATE TABLE hand_seats (
    hand_id bigint NOT NULL REFERENCES hands (id) ON DELETE CASCADE,
    seat    smallint NOT NULL,
    name    text NOT NULL,
    cards   text[],
    bet     integer NOT NULL,
    won     integer NOT NULL,
    stack   integer NOT NULL,
    PRIMARY KEY (hand_id, seat)
);

-- +goose Down
DROP TABLE hand_seats;
DROP TABLE hands;
DROP TABLE players;
DROP TABLE rooms;
