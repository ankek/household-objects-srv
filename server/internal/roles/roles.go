package roles

const Owner = "owner"

const Member = "member"

func Valid(role string) bool {
	return role == Owner || role == Member
}
