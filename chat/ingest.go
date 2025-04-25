package chat

import (
	. "clack/common"
	"regexp"
	"strconv"
)

var (
	userMentionRegex    = regexp.MustCompile(`<@([0-9]+)>`)
	roleMentionRegex    = regexp.MustCompile(`<@&([0-9]+)>`)
	channelMentionRegex = regexp.MustCompile(`<#([0-9]+)>`)
	urlRegex            = regexp.MustCompile(`(https?://[^\s]+)`)
)

func ParseMessageContent(content string) (mentionedUsers []Snowflake, mentionedRoles []Snowflake, mentionedChannels []Snowflake, urls []string) {
	userMatches := userMentionRegex.FindAllStringSubmatch(content, -1)
	for _, match := range userMatches {
		if len(match) > 1 {
			userID, parseErr := strconv.ParseInt(match[1], 10, 64)
			if parseErr != nil {
				continue
			}
			mentionedUsers = append(mentionedUsers, Snowflake(userID))
		}
	}

	roleMatches := roleMentionRegex.FindAllStringSubmatch(content, -1)
	for _, match := range roleMatches {
		if len(match) > 1 {
			roleID, parseErr := strconv.ParseInt(match[1], 10, 64)
			if parseErr != nil {
				continue
			}
			mentionedRoles = append(mentionedRoles, Snowflake(roleID))
		}
	}

	channelMatches := channelMentionRegex.FindAllStringSubmatch(content, -1)
	for _, match := range channelMatches {
		if len(match) > 1 {
			channelID, parseErr := strconv.ParseInt(match[1], 10, 64)
			if parseErr != nil {
				continue
			}
			mentionedChannels = append(mentionedChannels, Snowflake(channelID))
		}
	}

	urlMatches := urlRegex.FindAllStringSubmatch(content, -1)
	for _, match := range urlMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}

	return mentionedUsers, mentionedRoles, mentionedChannels, urls
}
