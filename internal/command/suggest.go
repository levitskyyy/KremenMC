package command

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
)

// Completion is a set of suggestions replacing Text[Start:Start+Length].
type Completion struct {
	Start, Length int
	Suggestions   []api.Suggestion
}

type suggestion struct {
	start, end int
	text       string
	tooltip    *api.Component
}

type builder struct {
	input string
	start int
	out   []suggestion
}

func (b *builder) remaining() string      { return b.input[b.start:] }
func (b *builder) remainingLower() string { return strings.ToLower(b.remaining()) }

func (b *builder) add(text string, tip *api.Component) {
	if text == b.remaining() {
		return
	}
	b.out = append(b.out, suggestion{start: b.start, end: len(b.input), text: text, tooltip: tip})
}

func (b *builder) addAt(start int, text string) {
	if text == b.input[start:] {
		return
	}
	b.out = append(b.out, suggestion{start: start, end: len(b.input), text: text})
}

// suggestMatching adds every candidate that the remaining input prefixes.
func (b *builder) suggestMatching(cands []string) {
	rem := b.remainingLower()
	for _, c := range cands {
		if strings.HasPrefix(strings.ToLower(c), rem) {
			b.add(c, nil)
		}
	}
}

// suggestKeys matches either the full key or its path for the default namespace.
func (b *builder) suggestKeys(keys []string) {
	rem := b.remainingLower()
	for _, k := range keys {
		if strings.HasPrefix(k, rem) || strings.HasPrefix(api.Key(k).Path(), rem) && !strings.Contains(rem, ":") {
			b.add(k, nil)
		}
	}
}

// Suggest computes tab completions for text, which may start with '/'.
// Start and Length index into text as given.
func (d *Dispatcher) Suggest(sender api.CommandSender, text string) Completion {
	offset := 0
	if strings.HasPrefix(text, "/") {
		offset = 1
	}
	input := text[offset:]
	if c, ok := d.foreignFor(input); ok && strings.Contains(input, " ") {
		if c.foreign.Suggest == nil || !d.canUse(c.node, sender) {
			return Completion{Start: len(text)}
		}
		comp := c.foreign.Suggest(sender, input)
		comp.Start += offset
		return comp
	}
	res := d.parse(d.tree.Load(), sender, input)
	ctx, parent, start := findSuggestionContext(res.ctx, len(input))
	start = min(start, len(input))
	var all []suggestion
	for _, child := range parent.Children {
		if !d.canUse(child, sender) {
			continue
		}
		b := &builder{input: input, start: start}
		d.listSuggestions(child, ctx, b)
		all = append(all, b.out...)
	}
	return merge(input, all, offset)
}

func (d *Dispatcher) Complete(sender api.CommandSender, line string) []api.Suggestion {
	return d.Suggest(sender, line).Suggestions
}

// findSuggestionContext mirrors brigadier's CommandContextBuilder method of
// the same name: it returns the context whose node is the parent of the
// argument at cursor, and where that argument starts.
func findSuggestionContext(c *cmdContext, cursor int) (*cmdContext, *api.CommandNode, int) {
	if c.rangeEnd() < cursor {
		if c.child != nil {
			return findSuggestionContext(c.child, cursor)
		}
		if len(c.nodes) > 0 {
			last := c.nodes[len(c.nodes)-1]
			return c, last.node, last.end + 1
		}
		return c, c.root, c.start
	}
	prev := c.root
	for i, n := range c.nodes {
		if n.start <= cursor && cursor <= n.end {
			trimmed := c.copy()
			trimmed.nodes = trimmed.nodes[:i]
			trimmed.args = slices.DeleteFunc(trimmed.args, func(a argValue) bool { return a.start >= n.start })
			return trimmed, prev, n.start
		}
		prev = n.node
	}
	return c, prev, c.start
}

func merge(input string, all []suggestion, offset int) Completion {
	if len(all) == 0 {
		return Completion{Start: len(input) + offset}
	}
	start, end := all[0].start, all[0].end
	for _, s := range all[1:] {
		start, end = min(start, s.start), max(end, s.end)
	}
	seen := map[string]bool{}
	out := make([]api.Suggestion, 0, len(all))
	for _, s := range all {
		t := input[start:s.start] + s.text + input[s.end:end]
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, api.Suggestion{Text: t, Tooltip: s.tooltip})
	}
	slices.SortStableFunc(out, func(a, b api.Suggestion) int {
		return strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text))
	})
	return Completion{Start: start + offset, Length: end - start, Suggestions: out}
}

func (d *Dispatcher) listSuggestions(n *api.CommandNode, ctx *cmdContext, b *builder) {
	if n.Kind == api.NodeLiteral {
		if strings.HasPrefix(n.Name, b.remainingLower()) {
			b.add(n.Name, nil)
		}
		return
	}
	if n.Suggest != nil {
		sctx := ctx.copy()
		sctx.input = b.input
		for _, s := range n.Suggest(sctx, b.remaining()) {
			b.add(s.Text, s.Tooltip)
		}
		return
	}
	d.typeSuggestions(n.Type, ctx, b)
}

func (d *Dispatcher) typeSuggestions(t api.ArgumentType, ctx *cmdContext, b *builder) {
	switch canonicalParser(t.Parser) {
	case api.ParserBool:
		b.suggestMatching([]string{"true", "false"})
	case api.ParserEntity:
		d.suggestEntities(t, ctx, b)
	case api.ParserGameProfile:
		d.suggestEntities(api.ArgumentType{PlayersOnly: true}, ctx, b)
	case api.ParserBlockPos, api.ParserVec3:
		suggestCoords(b, 3)
	case api.ParserColumnPos, api.ParserVec2, api.ParserRotation:
		suggestCoords(b, 2)
	case ParserAngle:
		suggestCoords(b, 1)
	case api.ParserGamemode:
		b.suggestMatching([]string{"survival", "creative", "adventure", "spectator"})
	case ParserTeamColor:
		b.suggestMatching(teamColors)
	case ParserEntityAnchor:
		b.suggestMatching([]string{"eyes", "feet"})
	case api.ParserDimension:
		if d.server != nil {
			var keys []string
			for _, w := range d.server.Worlds() {
				keys = append(keys, string(w.Key().Normalize()))
			}
			b.suggestKeys(keys)
		}
	case api.ParserTime:
		rem := b.remaining()
		if _, err := strconv.ParseFloat(rem, 32); err == nil {
			for _, u := range []string{"d", "s", "t"} {
				b.add(rem+u, nil)
			}
		}
	case api.ParserBlockState, ParserBlockPredicate:
		if !strings.ContainsAny(b.remaining(), "[{") {
			b.suggestKeys(d.blockNames.get(d.listBlocks))
		}
	case api.ParserItemStack, ParserItemPredicate:
		if !strings.ContainsAny(b.remaining(), "[{") {
			b.suggestKeys(d.itemNames.get(d.listItems))
		}
	}
}

func suggestCoords(b *builder, n int) {
	rem := b.remaining()
	var cands []string
	if rem == "" {
		for i := 1; i <= n; i++ {
			cands = append(cands, strings.TrimSpace(strings.Repeat("~ ", i)))
		}
	} else {
		fields := strings.Split(strings.TrimRight(rem, " "), " ")
		if len(fields) < n {
			base := strings.Join(fields, " ")
			for i := len(fields) + 1; i <= n; i++ {
				base += " ~"
				cands = append(cands, base)
			}
		}
	}
	b.suggestMatching(cands)
}

func (d *Dispatcher) suggestEntities(t api.ArgumentType, ctx *cmdContext, b *builder) {
	rem := b.remaining()
	if strings.HasPrefix(rem, "@") {
		if !d.selectorsAllowed(ctx.sender) {
			return
		}
		if i := strings.LastIndexAny(rem, "[,"); i >= 0 {
			opt := rem[i+1:]
			if !strings.Contains(opt, "=") && !strings.Contains(rem, "]") {
				ob := &builder{input: b.input, start: b.start + i + 1}
				var keys []string
				for _, k := range selectorOptions {
					keys = append(keys, k+"=")
				}
				ob.suggestMatching(keys)
				b.out = append(b.out, ob.out...)
			}
			return
		}
	}
	var cands []string
	if d.selectorsAllowed(ctx.sender) {
		kinds := []string{"@p", "@a", "@r", "@s", "@e", "@n"}
		if t.PlayersOnly {
			kinds = kinds[:4]
		}
		cands = append(cands, kinds...)
	}
	if d.server != nil {
		viewer, _ := ctx.sender.(api.Player)
		for _, p := range d.server.Players() {
			if viewer == nil || viewer.CanSee(p) {
				cands = append(cands, p.Name())
			}
		}
	}
	b.suggestMatching(cands)
}

var selectorOptions = []string{"name", "distance", "level", "limit", "sort", "type", "gamemode", "tag", "x", "y", "z", "dx", "dy", "dz"}

type lazyKeys struct {
	once sync.Once
	keys []string
}

func (l *lazyKeys) get(fill func() []string) []string {
	l.once.Do(func() { l.keys = fill() })
	return l.keys
}

func (d *Dispatcher) listBlocks() []string {
	if d.server == nil || d.server.Blocks() == nil {
		return nil
	}
	reg := d.server.Blocks()
	seen := map[api.Key]bool{}
	var out []string
	for id := uint32(0); ; id++ {
		st, ok := reg.ByID(id)
		if !ok {
			break
		}
		k := st.Type().Normalize()
		if !seen[k] {
			seen[k] = true
			out = append(out, string(k))
		}
	}
	slices.Sort(out)
	return out
}

func (d *Dispatcher) listItems() []string {
	if d.server == nil || d.server.Items() == nil {
		return nil
	}
	reg := d.server.Items()
	var out []string
	for id := int32(0); ; id++ {
		k, ok := reg.ByID(id)
		if !ok {
			break
		}
		out = append(out, string(k.Normalize()))
	}
	slices.Sort(out)
	return out
}
