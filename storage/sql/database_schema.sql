CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL,
    nickname TEXT DEFAULT NULL,
    status INTEGER NOT NULL DEFAULT 0,
    hash TEXT NOT NULL,
    salt TEXT NOT NULL,
    email TEXT,
    invite_code TEXT,
    UNIQUE (username)
);

CREATE TABLE roles (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    color INTEGER NOT NULL,
    position INTEGER NOT NULL,
    permissions INTEGER DEFAULT 0,
    hoisted INTEGER DEFAULT 0,
    mentionable INTEGER DEFAULT 0
);

CREATE TABLE user_roles (
    user_id INTEGER NOT NULL,
    role_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE TABLE user_tokens (
    user_id INTEGER NOT NULL,
    token TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, token),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
)

CREATE TABLE user_invite_codes (
    user_id INTEGER NOT NULL,
    invite_code TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER,
    uses INTEGER NOT NULL DEFAULT 0,
    max_uses INTEGER,
    invalidated INTEGER DEFAULT 0,
    PRIMARY KEY (user_id, invite_code),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
)

CREATE TABLE channels (
    id INTEGER PRIMARY KEY,
    type INTEGER NOT NULL,
    name TEXT,
    description TEXT,
    position INTEGER,
    parent_id INTEGER,
    FOREIGN KEY (parent_id) REFERENCES channels(id) ON DELETE SET NULL
);

CREATE TABLE channel_role_permissions (
    channel_id INTEGER NOT NULL,
    role_id INTEGER NOT NULL,
    allow INTEGER DEFAULT 0 NOT NULL,
    deny INTEGER DEFAULT 0 NOT NULL,
    PRIMARY KEY (channel_id, role_id),
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE TABLE channel_user_permissions (
    channel_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    allow INTEGER DEFAULT 0 NOT NULL,
    deny INTEGER DEFAULT 0 NOT NULL,
    PRIMARY KEY (channel_id, user_id),
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE emojis (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE messages (
    id INTEGER PRIMARY KEY,
    type INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    pinned INTEGER DEFAULT 0 NOT NULL,
    author_id INTEGER,
    reference_id INTEGER,
    content TEXT,
    edited_timestamp INTEGER,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
    FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (reference_id) REFERENCES messages(id) ON DELETE SET NULL
);

CREATE TABLE message_user_mentions (
    message_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (message_id, user_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE message_role_mentions (
    message_id INTEGER NOT NULL,
    role_id INTEGER NOT NULL,
    PRIMARY KEY (message_id, role_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE TABLE message_channel_mentions (
    message_id INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    PRIMARY KEY (message_id, channel_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE TABLE embeds (
    id INTEGER PRIMARY KEY,
    message_id INTEGER NOT NULL,
    type INTEGER NOT NULL,
    url TEXT,
    title TEXT,
    description TEXT,
    color INTEGER,
    timestamp INTEGER,
    image_url TEXT,
    image_id INTEGER,
    thumbnail_url TEXT,
    thumbnail_id INTEGER,
    video_url TEXT,
    video_id INTEGER,
    author_name TEXT,
    author_url TEXT,
    author_icon_url TEXT,
    author_icon_id INTEGER,
    provider_name TEXT,
    provider_url TEXT,
    footer_text TEXT,
    footer_icon_url TEXT,
    footer_icon_id INTEGER,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE TABLE embed_fields (
    embed_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    inline INTEGER DEFAULT 0,
    UNIQUE (embed_id, name),
    FOREIGN KEY (embed_id) REFERENCES embeds(id) ON DELETE CASCADE
);

CREATE TABLE reactions (
    emoji_id INTEGER NOT NULL,
    message_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (emoji_id, message_id, user_id)
);

CREATE TABLE attachments (
    id INTEGER PRIMARY KEY,
    message_id INTEGER NOT NULL,
    type INTEGER NOT NULL,
    mimetype TEXT NOT NULL,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE TABLE settings (
    id INTEGER PRIMARY KEY,
    site_name TEXT NOT NULL DEFAULT '',
    login_message TEXT NOT NULL DEFAULT '',
    default_permissions INTEGER DEFAULT 0,
    uses_email INTEGER DEFAULT 0,
    uses_invite_code INTEGER DEFAULT 0,
    uses_captcha INTEGER DEFAULT 0,
    uses_login_captcha INTEGER DEFAULT 0,
    captcha_site_key TEXT NOT NULL DEFAULT '',
    captcha_secret_key TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO settings(id) VALUES (0);

CREATE TABLE previews (
    id INTEGER PRIMARY KEY, -- Attachment ID or Embed Image/Thumbnail/Video/Etc ID
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    preload TEXT NOT NULL -- Base64 encoded WebP preload image
);

-- Indexes
CREATE INDEX idx_messages_channel_id ON messages(channel_id);
CREATE INDEX idx_embeds_message_id ON embeds(message_id);
CREATE INDEX idx_reactions_message_id ON reactions(message_id, emoji_id);
CREATE INDEX idx_reactions_user_id ON reactions(message_id, emoji_id, user_id);
CREATE INDEX idx_attachments_message_id ON attachments(message_id);
CREATE INDEX idx_embed_fields_embed_id ON embed_fields(embed_id);

-- Views
CREATE VIEW external_urls AS
SELECT
    id,
    video_url as external_url
FROM
    embeds
WHERE
    video_url IS NOT NULL;

-- Triggers
CREATE TRIGGER delete_reactions_on_emoji_delete
AFTER DELETE ON emojis
FOR EACH ROW
BEGIN
    DELETE FROM reactions WHERE emoji_id = OLD.id;
END;