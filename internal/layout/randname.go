// このファイルは、task 名やブランチ名の初期値に使う「形容詞-名詞」のランダムな名前(brave-otter のような)を作る。
// 単語はすべて小文字の ASCII だけなので、結果はそのまま task 名にも git のブランチ名にも使える。
package layout

// adjectives は名前の前半に使う形容詞の一覧。
//
// 「var 名前 = []string{...}」は、文字列のスライス(可変長配列。TypeScript の string[])をパッケージ変数として定義している。
// パッケージ変数はファイル内の関数から自由に参照でき、プログラム起動時に初期化される。
// 要素の最後にもカンマが要るのは Go の文法で、行ごとに並べても末尾に , を付ければ改行できる。
var adjectives = []string{
	"brave", "bright", "calm", "clever", "cosmic", "cozy", "crisp", "curious",
	"eager", "fancy", "fast", "fluffy", "gentle", "golden", "happy", "humble",
	"jolly", "keen", "kind", "lively", "lucky", "mellow", "merry", "mighty",
	"nimble", "noble", "patient", "peaceful", "playful", "polite", "proud", "quick",
	"quiet", "rapid", "shiny", "silent", "silver", "smooth", "snowy", "steady",
	"sunny", "swift", "tidy", "tiny", "vivid", "warm", "wise", "witty",
}

// nouns は名前の後半に使う名詞(動植物や自然の語)の一覧。
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
//
// 乱数の生成そのものは引数 intn(関数)に任せている。func(n int) int は
// 「整数 n を受け取り、0 以上 n 未満の整数を返す関数」という型で、TypeScript の (n: number) => number に相当する。
// 本番では math/rand の rand.IntN のような関数を渡し、テストでは決まった値を返す関数を渡せるので、結果を固定してテストできる。
// 使い方: layout.RandomName(rand.IntN) → "brave-otter"。
// len(adjectives) は要素数で、adjectives[i] は i 番目の要素(0 始まり。TypeScript の配列と同じ)。
func RandomName(intn func(n int) int) string {
	return adjectives[intn(len(adjectives))] + "-" + nouns[intn(len(nouns))]
}
