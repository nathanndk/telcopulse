CREATE INDEX notification_dead_letters_recent_idx
 ON notification.dead_letters(created_at DESC,topic DESC,partition_id DESC,message_offset DESC);
