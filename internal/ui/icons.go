package ui

// icons is the glyph set used in the UI. The design uses Nerd Font (Font
// Awesome) glyphs; "ascii" is a fallback for terminals without a patched font.
type icons struct {
	Bell, Chat, Teams, Calendar, Files, Search, Help, Settings string
	Arrow, Hash, Clip, More, Video, Check, ThumbsUp, File      string
	Folder, Phone, Reply, Bolt, External, Pencil, Lock         string
	Download, Users, Upload, Prompt, Copy, Trash, Smile, At    string
}

func newIcons(set string) icons {
	if set == "ascii" {
		return icons{
			Bell: "!", Chat: "c", Teams: "t", Calendar: "d", Files: "f", Search: "/", Help: "?", Settings: "*",
			Arrow: "", Hash: "#", Clip: "+", More: "...", Video: ">", Check: "v", ThumbsUp: "+", File: "-",
			Folder: "=", Phone: "@", Reply: "<", Bolt: "*", External: "^", Pencil: "~", Lock: "x",
			Download: "v", Users: "&", Upload: "^", Prompt: ">", Copy: "=", Trash: "x", Smile: ":)", At: "@",
		}
	}
	return icons{
		Bell: "", Chat: "", Teams: "", Calendar: "", Files: "", Search: "",
		Help: "", Settings: "", Arrow: "", Hash: "", Clip: "", More: "",
		Video: "", Check: "", ThumbsUp: "", File: "", Folder: "", Phone: "",
		Reply: "", Bolt: "", External: "", Pencil: "", Lock: "", Download: "",
		Users: "", Upload: "", Prompt: "›", Copy: "", Trash: "", Smile: "", At: "@",
	}
}
