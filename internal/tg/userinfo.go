package tg

import (
	"fmt"
	"strings"

	tgapi "github.com/gotd/td/tg"
)

// newUserInfo builds UserInfo from a user and its full info. f may be nil;
// then only the fields of u are filled.
func newUserInfo(u *tgapi.User, f *tgapi.UserFull) UserInfo {
	info := UserInfo{
		ID:            u.ID,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Phone:         u.Phone,
		LangCode:      u.LangCode,
		Contact:       u.Contact,
		MutualContact: u.MutualContact,
		CloseFriend:   u.CloseFriend,
		Bot:           u.Bot,
		Premium:       u.Premium,
		Verified:      u.Verified,
		Scam:          u.Scam,
		Fake:          u.Fake,
		Support:       u.Support,
		Deleted:       u.Deleted,
		Restricted:    u.Restricted,
	}
	info.Username, info.Usernames = usernames(u)
	info.Status, info.LastSeen = userStatus(u.Status)
	for _, r := range u.RestrictionReason {
		info.RestrictionReasons = append(info.RestrictionReasons, r.Platform+": "+r.Reason+": "+r.Text)
	}
	_, info.HasPhoto = u.Photo.(*tgapi.UserProfilePhoto)

	if f == nil {
		return info
	}
	info.Bio = f.About
	info.Blocked = f.Blocked
	info.CommonChatsCount = f.CommonChatsCount
	if b, ok := f.GetBirthday(); ok {
		info.Birthday = &Birthday{Day: b.Day, Month: b.Month, Year: b.Year}
	}
	if n, ok := f.GetNote(); ok {
		info.Note = n.Text
	}
	if id, ok := f.GetPersonalChannelID(); ok {
		info.PersonalChannelID = id
	}
	if bi, ok := f.GetBotInfo(); ok {
		info.BotDescription = bi.Description
	}
	info.Business = business(f)
	return info
}

// usernames returns the primary username and the other active ones.
func usernames(u *tgapi.User) (string, []string) {
	primary := u.Username
	var others []string
	for _, n := range u.Usernames {
		if !n.Active {
			continue
		}
		if primary == "" {
			primary = n.Username
			continue
		}
		if n.Username != primary {
			others = append(others, n.Username)
		}
	}
	return primary, others
}

func userStatus(s tgapi.UserStatusClass) (string, int64) {
	switch v := s.(type) {
	case *tgapi.UserStatusOnline:
		return "online", 0
	case *tgapi.UserStatusOffline:
		return "offline", int64(v.WasOnline)
	case *tgapi.UserStatusRecently:
		return "recently", 0
	case *tgapi.UserStatusLastWeek:
		return "last_week", 0
	case *tgapi.UserStatusLastMonth:
		return "last_month", 0
	}
	return "unknown", 0
}

func business(f *tgapi.UserFull) *Business {
	var b Business
	if loc, ok := f.GetBusinessLocation(); ok {
		b.Address = loc.Address
	}
	if intro, ok := f.GetBusinessIntro(); ok {
		var parts []string
		for _, p := range []string{intro.Title, intro.Description} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		b.Intro = strings.Join(parts, "\n")
	}
	if wh, ok := f.GetBusinessWorkHours(); ok && len(wh.WeeklyOpen) > 0 {
		hours := &WorkHours{Timezone: wh.TimezoneID}
		for _, o := range wh.WeeklyOpen {
			hours.Open = append(hours.Open, openInterval(o.StartMinute, o.EndMinute))
		}
		b.WorkHours = hours
	}
	if b == (Business{}) {
		return nil
	}
	return &b
}

var weekdays = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// openInterval formats minutes from Monday 00:00 as "Mon 09:00-18:00".
// An interval up to one day names only the start day; the end may be past
// midnight. A longer interval names the end day too: "Mon 00:00-Sat 00:00".
func openInterval(start, end int) string {
	const day = 24 * 60
	from := weekdays[(start/day)%7] + " " + clock(start%day)
	if end-start <= day {
		return from + "-" + clock(end%day)
	}
	return from + "-" + weekdays[(end/day)%7] + " " + clock(end%day)
}

func clock(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}
