-- Mattermost stores Posts.Message in a varchar column and derives the runtime
-- post size limit from this column length divided by 4 for worst-case UTF-8.
-- 200000 keeps the effective Mattermost message limit at 50000 runes.
alter table posts
	alter column message type varchar(200000);
