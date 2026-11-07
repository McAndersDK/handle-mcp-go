package match

import (
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

// Words that only say how a thing is made or packed: "frilandsæg" is æg, "splitlinser" is linser.
// Not "baby": "Babymajs" is another vegetable than "majs".
var describers = []string{"frilands", "skrabe", "øko", "split", "mini", "snack"}

// A part inside a compound: "kyllingeoverlår" and "kyllingeunderlår" are kyllingelår.
var parts = []string{"over", "under"}

var limbs = []string{"lår"} // only before these: never "kartoverofler"

// A cut of the thing named first: "makrelfilet" and "makrelstykker" are makrel.
var cuts = []string{"filet", "fileter", "stykke", "stykker", "strimler", "tern"}

var nutrients = map[string]bool{"fedt": true, "protein": true, "sukker": true, "kakao": true,
	"alkohol": true, "salt": true, "frugt": true, "kulhydrat": true}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// kc is name.replace("C", "K").replace("c", "k").lower(): c and k are the same sound in Danish
// shop names: "Clementiner" ~ "klementiner".
func kc(s string) string {
	return pystr.Lower(strings.NewReplacer("C", "K", "c", "k").Replace(s))
}

// Score is 1.0 when every word is a whole word in the name, or its Danish plural or singular
// ('bananer' ~ 'banan'). Half for a word inside another word, or only in the brand: 'Fior di
// Frutta' is jam, not 'fior di latte'. A quarter for a word only in the flavour part of the
// name ('Yoghurt m. banan'), unless the item asks for it there (`asked`: 'makrel i tomat').
func Score(wordList []string, p *model.Product, asked []string) float64 {
	ws := make([]string, len(wordList))
	for i, w := range wordList {
		ws[i] = strings.ReplaceAll(w, "c", "k")
	}
	ask := make([]string, len(asked))
	for i, w := range asked {
		ask[i] = strings.ReplaceAll(w, "c", "k")
	}
	name, brand := kc(p.Name), kc(str(p.Brand))
	head, _, _ := splitFlavour(name)
	headWords := words(head)
	// A compound split in two: "Cherry tomater" is "cherrytomater".
	tokens := append([]string(nil), headWords...)
	for i := 1; i < len(headWords); i++ {
		tokens = append(tokens, headWords[i-1]+headWords[i])
	}
	brandTokens, tailTokens := words(brand), words(name)
	sum := 0.0
	for _, w := range ws {
		switch {
		case contains(tokens, w) || anyToken(tokens, func(t string) bool { return plural(w, t) || described(w, t) }):
			sum += 1.0
		case anyToken(tokens, func(t string) bool { return compound(w, t) }):
			sum += 0.75 // 'frilandsæg' for 'æg': better than a word inside, below whole
		case strings.Contains(head, w) || stemHit(w, tokens) || contains(brandTokens, w) || stemHit(w, brandTokens):
			sum += 0.5
		case contains(tailTokens, w) || stemHit(w, tailTokens):
			if contains(ask, w) {
				sum += 1.0
			} else {
				sum += 0.25
			}
		case strings.Contains(name, w):
			sum += 0.25
		}
	}
	if len(ws) == 0 {
		return 0.0
	}
	return sum / float64(len(ws))
}

func anyToken(tokens []string, f func(string) bool) bool {
	for _, t := range tokens {
		if f(t) {
			return true
		}
	}
	return false
}

// plural: Danish singular and plural are the same product: 'pære' ~ 'pærer', 'banan' ~ 'bananer'.
func plural(word, token string) bool {
	short, long := word, token
	if pystr.Len(token) < pystr.Len(word) {
		short, long = token, word
	}
	return pystr.Len(short) >= 3 && (long == short+"r" || long == short+"er" || long == short+"e")
}

// described: 'frilandsæg' is plain æg: the first part only says how it is made. 'makrelfilet' is
// makrel, 'laksestykke' is 'laksefilet' and 'kyllingeoverlår' is 'kyllingelår'.
func described(word, token string) bool {
	wr := []rune(word)
	// 'laksefilet' → 'lakse': then 'laksestykke' is the same fish, cut another way.
	bases := []string{word}
	for _, c := range cuts {
		if strings.HasSuffix(word, c) && len(wr) > pystr.Len(c)+2 {
			bases = append(bases, string(wr[:len(wr)-pystr.Len(c)]))
		}
	}
	for _, d := range describers {
		if eq2(token, d, word) {
			return true
		}
	}
	for _, b := range bases {
		for _, c := range cuts {
			if eq2(token, b, c) || eq3(token, b, "e", c) || eq3(token, b, "s", c) {
				return true
			}
		}
	}
	for _, b := range limbs {
		if strings.HasSuffix(word, b) {
			stem := string(wr[:len(wr)-pystr.Len(b)])
			for _, m := range parts {
				if eq3(token, stem, m, b) {
					return true
				}
			}
		}
	}
	return false
}

// compound: Danish compound words name the thing last: 'frilandsæg' is æg, 'splitlinser' is linser.
// The token needs 3 more letters, so 'post' is not 'ost'.
func compound(word, token string) bool {
	return pystr.Len(token) >= pystr.Len(word)+3 && strings.HasSuffix(token, word)
}

func stemHit(word string, tokens []string) bool {
	return anyToken(tokens, func(t string) bool {
		return pystr.Len(t) >= 4 && (strings.HasPrefix(word, t) || strings.HasPrefix(t, word))
	})
}

// Mixed is 1 for a blend the item does not ask for: 'Hakket oksekød m. 50% grønt'. A share of
// another food ('50% grønt') means a mix. A share of fat or sugar ('m. 0,5% fedt') does not,
// nor 'Rugbrød m. kerner' or 'Laks m. skind'.
func Mixed(wordList []string, p *model.Product) int {
	_, after, found := splitFlavour(p.Name)
	if !found {
		return 0
	}
	other := false
	for _, w := range blendWords(after) {
		if !nutrients[pystr.Lower(w)] {
			other = true
			break
		}
	}
	if !other {
		return 0
	}
	tail := words(after)
	for _, w := range wordList {
		if contains(tail, w) || stemHit(w, tail) {
			return 0
		}
	}
	return 1
}

// FlavourOnly is true when the item's words are only in the flavour part: 'Grøn te m. mandarin'.
func FlavourOnly(wordList []string, p *model.Product) bool {
	if Score(wordList, p, nil) <= 0 {
		return false
	}
	for _, w := range wordList {
		if Score([]string{w}, p, nil) >= 0.5 {
			return false
		}
	}
	return true
}

// BrandOnly is true when only the brand has the item's words: 'Fior di Frutta' jam for 'fior di latte'.
func BrandOnly(wordList []string, p *model.Product) bool {
	bare := *p
	bare.Brand = nil
	return Score(wordList, &bare, nil) == 0 && Score(wordList, p, nil) > 0
}

// CategoryRank is 0 when an item word fits the store's category ('cherrytomater' ~ 'Tomater'), else 1.
func CategoryRank(wordList []string, p *model.Product) int {
	tokens := words(str(p.Category))
	for _, w := range wordList {
		if stemHit(w, tokens) || anyToken(tokens, func(t string) bool { return suffix(w, t) }) {
			return 0
		}
	}
	return 1
}

// suffix: Danish compound words: 'cherrytomater' ends with 'tomater'. Short tokens like 'ost' are too
// loose ('post'), so both sides need 4+ letters.
func suffix(word, token string) bool {
	return pystr.Len(token) >= 4 && pystr.Len(word) >= 4 &&
		(strings.HasSuffix(word, token) || strings.HasSuffix(token, word))
}

// eq2 and eq3 tell whether token is a+b or a+b+c, without building the string.
func eq2(token, a, b string) bool {
	return len(token) == len(a)+len(b) && strings.HasPrefix(token, a) && strings.HasSuffix(token, b)
}

func eq3(token, a, b, c string) bool {
	return len(token) == len(a)+len(b)+len(c) && strings.HasPrefix(token, a) && strings.HasSuffix(token, c) &&
		token[len(a):len(token)-len(c)] == b
}
