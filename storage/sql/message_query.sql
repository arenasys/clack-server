SELECT
    m.id,
    m.type,
    m.channel_id,
    m.timestamp,
    m.pinned,
    m.author_id,
    m.reference_id,
    m.content,
    m.edited_timestamp,
    
    -- Aggregate Attachments
    (
        SELECT json_group_array(
            json_object(
                'id', a.id,
                'filename', a.filename,
                'type', a.type,
                'size', s.sz,
                'preload', webp_base64(s2.data),
                'width', p.width,
                'height', p.height
            )
        )
        FROM attachments a
        LEFT JOIN sqlar s ON a.path = s.name
        LEFT JOIN previews p ON a.id = p.id
        LEFT JOIN sqlar s2 ON p.blur = s2.name
        WHERE a.message_id = m.id
    ) AS attachments,
    
    -- Aggregate Embeds
    (
        SELECT json_group_array(
            json_object(
                'id', e.id,
                'type', e.type,
                'url', e.url,
                'title', e.title,
                'description', e.description,
                'color', e.color,
                'timestamp', e.timestamp,
                
                -- Nested Image Data
                'image', CASE 
                    WHEN e.image_id IS NULL THEN NULL 
                    ELSE json_object(
                        'id', e.image_id,
                        'url', e.image_url,
                        'width', img_p.width,
                        'height', img_p.height,
                        'preload', webp_base64(img_s.data)
                    ) 
                END,
                
                -- Nested Thumbnail Data
                'thumbnail', CASE 
                    WHEN e.thumbnail_id IS NULL THEN NULL 
                    ELSE json_object(
                        'id', e.thumbnail_id,
                        'url', e.thumbnail_url,
                        'width', thumb_p.width,
                        'height', thumb_p.height,
                        'preload', webp_base64(thumb_s.data)
                    )
                END,
                
                -- Nested Video Data
                'video', CASE 
                    WHEN e.video_id IS NULL THEN NULL 
                    ELSE json_object(
                        'id', e.video_id,
                        'url', e.video_url,
                        'width', vid_p.width,
                        'height', vid_p.height,
                        'preload', webp_base64(vid_s.data)
                    )
                END,
                
                -- Nested Author Data
                'author', CASE
                    WHEN e.author_name IS NULL AND e.author_icon_id IS NULL THEN NULL
                    ELSE json_object(
                        'name', e.author_name,
                        'url', e.author_url,
                        'icon', CASE
                            WHEN e.author_icon_id IS NULL THEN NULL
                            ELSE json_object(
                                'id', e.author_icon_id,
                                'url', e.author_icon_url,
                                'width', author_icon_p.width,
                                'height', author_icon_p.height,
                                'preload', webp_base64(author_icon_s.data)
                            )
                        END
                    )
                END,
                
                -- Nested Provider Data
                'provider', CASE 
                    WHEN e.provider_name IS NULL THEN NULL
                    ELSE json_object(
                        'name', e.provider_name,
                        'url', e.provider_url
                    )
                END,
                
                -- Nested Footer Data
                'footer', CASE
                    WHEN e.footer_text IS NULL AND e.footer_icon_id IS NULL THEN NULL
                    ELSE json_object(
                        'text', e.footer_text,
                        'icon', CASE
                            WHEN e.footer_icon_id IS NULL THEN NULL
                            ELSE json_object(
                                'id', e.footer_icon_id,
                                'url', e.footer_icon_url,
                                'width', footer_icon_p.width,
                                'height', footer_icon_p.height,
                                'preload', webp_base64(footer_icon_s.data)
                            )
                        END
                    )
                END,
                
                -- Embed Fields
                'fields', (
                    SELECT json_group_array(
                        json_object(
                            'name', f.name,
                            'value', f.value,
                            'inline', f.inline
                        )
                    )
                    FROM embed_fields f
                    WHERE f.embed_id = e.id
                )
            )
        )
        FROM embeds e
        LEFT JOIN previews img_p ON e.image_id = img_p.id
        LEFT JOIN sqlar img_s ON img_p.blur = img_s.name
        LEFT JOIN previews thumb_p ON e.thumbnail_id = thumb_p.id
        LEFT JOIN sqlar thumb_s ON thumb_p.blur = thumb_s.name
        LEFT JOIN previews vid_p ON e.video_id = vid_p.id
        LEFT JOIN sqlar vid_s ON vid_p.blur = vid_s.name
        LEFT JOIN previews author_icon_p ON e.author_icon_id = author_icon_p.id
        LEFT JOIN sqlar author_icon_s ON author_icon_p.blur = author_icon_s.name
        LEFT JOIN previews footer_icon_p ON e.footer_icon_id = footer_icon_p.id
        LEFT JOIN sqlar footer_icon_s ON footer_icon_p.blur = footer_icon_s.name
        WHERE e.message_id = m.id
    ) AS embeds,
    
    -- Aggregate Reactions
    (
        SELECT json_group_array(
            json_object(
                'emoji', r.emoji,
                'emoji_id', r.emoji_id,
                'users', (
                    SELECT json_group_array(r2.user_id)
                    FROM reactions r2
                    WHERE r2.message_id = m.id AND r2.emoji = r.emoji
                )
            )
        )
        FROM reactions r
        WHERE r.message_id = m.id
    ) AS reactions,
    
    -- Aggregate Mentions
    (
        SELECT json_group_array(user_id)
        FROM message_user_mentions
        WHERE message_id = m.id
    ) AS mentioned_users,
    
    (
        SELECT json_group_array(role_id)
        FROM message_role_mentions
        WHERE message_id = m.id
    ) AS mentioned_roles,
    
    (
        SELECT json_group_array(channel_id)
        FROM message_channel_mentions
        WHERE message_id = m.id
    ) AS mentioned_channels

FROM
    messages m;