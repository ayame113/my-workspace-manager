package layout

var adjectives = []string{
	"brave", "bright", "calm", "clever", "cosmic", "cozy", "crisp", "curious",
	"eager", "fancy", "fast", "fluffy", "gentle", "golden", "happy", "humble",
	"jolly", "keen", "kind", "lively", "lucky", "mellow", "merry", "mighty",
	"nimble", "noble", "patient", "peaceful", "playful", "polite", "proud", "quick",
	"quiet", "rapid", "shiny", "silent", "silver", "smooth", "snowy", "steady",
	"sunny", "swift", "tidy", "tiny", "vivid", "warm", "wise", "witty",
}

var nouns = []string{
	"badger", "beaver", "breeze", "brook", "canyon", "cedar", "cloud", "comet",
	"coral", "crane", "daisy", "dolphin", "dragonfly", "eagle", "ember", "falcon",
	"fern", "finch", "forest", "fox", "glacier", "harbor", "heron", "island",
	"koala", "lagoon", "lantern", "maple", "meadow", "moon", "orchid", "otter",
	"panda", "pebble", "penguin", "pine", "rabbit", "river", "robin", "sparrow",
	"squirrel", "star", "summit", "swan", "tiger", "turtle", "valley", "willow",
}

// RandomName returns `<adjective>-<noun>`. intn must return a value in [0, n).
// Every word is lowercase ASCII letters only, so the result is a valid task name and a valid git branch name.
func RandomName(intn func(n int) int) string {
	return adjectives[intn(len(adjectives))] + "-" + nouns[intn(len(nouns))]
}
