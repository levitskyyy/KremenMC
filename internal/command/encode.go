package command

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

const (
	nodeRoot     = 0
	nodeLiteral  = 1
	nodeArgument = 2

	flagExecutable  = 0x04
	flagRedirect    = 0x08
	flagSuggestions = 0x10
	flagRestricted  = 0x20

	// SuggestAskServer makes the client request completions for a node.
	SuggestAskServer = "minecraft:ask_server"
)

// EncodeTree writes the body of ClientboundCommands for protocol 777 with
// only the nodes sender may use.
func (d *Dispatcher) EncodeTree(w *proto.Writer, sender api.CommandSender) {
	d.EncodeTreeWith(w, sender, Table777)
}

// EncodeTreeWith is EncodeTree with another protocol's parser table. Parsers
// missing from table are sent as a single-word brigadier:string.
func (d *Dispatcher) EncodeTreeWith(w *proto.Writer, sender api.CommandSender, table ParserTable) {
	root := d.tree.Load().root
	index := map[*api.CommandNode]int32{root: 0}
	order := []*api.CommandNode{root}
	children := map[*api.CommandNode][]*api.CommandNode{}
	for i := 0; i < len(order); i++ {
		n := order[i]
		var kids []*api.CommandNode
		for _, c := range n.Children {
			if !d.canUse(c, sender) {
				continue
			}
			kids = append(kids, c)
			if _, ok := index[c]; !ok {
				index[c] = int32(len(order))
				order = append(order, c)
			}
		}
		children[n] = kids
		if r := n.Redirect; r != nil && n != root && d.canUse(r, sender) {
			if _, ok := index[r]; !ok {
				index[r] = int32(len(order))
				order = append(order, r)
			}
		}
	}

	w.VarInt(int32(len(order)))
	for _, n := range order {
		var flags byte
		switch {
		case n == root:
			flags = nodeRoot
		case n.Kind == api.NodeLiteral:
			flags = nodeLiteral
		default:
			flags = nodeArgument
		}
		if n != root {
			if n.Executor != nil {
				flags |= flagExecutable
			}
			if n.Redirect != nil {
				if _, ok := index[n.Redirect]; ok {
					flags |= flagRedirect
				}
			}
			if n.Kind == api.NodeArgument && n.Suggest != nil {
				flags |= flagSuggestions
			}
			if d.restricted(n) {
				flags |= flagRestricted
			}
		}
		w.U8(flags)
		kids := children[n]
		w.VarInt(int32(len(kids)))
		for _, c := range kids {
			w.VarInt(index[c])
		}
		if flags&flagRedirect != 0 {
			w.VarInt(index[n.Redirect])
		}
		if n == root {
			continue
		}
		w.String(n.Name)
		if n.Kind == api.NodeArgument {
			writeParser(w, n.Type, table)
			if flags&flagSuggestions != 0 {
				w.String(SuggestAskServer)
			}
		}
	}
	w.VarInt(0)
}

// restricted marks nodes that vanilla clients refuse to run from click events
// without confirmation: anything gated by a permission that is not granted to
// everyone by default.
func (d *Dispatcher) restricted(n *api.CommandNode) bool {
	if n.Permission == "" {
		return false
	}
	if d.server != nil {
		if perms := d.server.Permissions(); perms != nil {
			if p, ok := perms.Get(n.Permission); ok {
				return p.Default != api.DefaultTrue
			}
		}
	}
	return true
}

func writeParser(w *proto.Writer, t api.ArgumentType, table ParserTable) {
	p := canonicalParser(t.Parser)
	id, ok := table.Lookup(p)
	if !ok {
		id, ok = table.Lookup(t.Parser)
	}
	if !ok {
		w.VarInt(table[api.ParserString])
		w.VarInt(int32(api.SingleWord))
		return
	}
	w.VarInt(id)
	switch p {
	case api.ParserFloat:
		writeBounds(w, t)
		if t.HasMin {
			w.F32(float32(t.Min))
		}
		if t.HasMax {
			w.F32(float32(t.Max))
		}
	case api.ParserDouble:
		writeBounds(w, t)
		if t.HasMin {
			w.F64(t.Min)
		}
		if t.HasMax {
			w.F64(t.Max)
		}
	case api.ParserInteger:
		writeBounds(w, t)
		if t.HasMin {
			w.I32(clampI32(t.Min))
		}
		if t.HasMax {
			w.I32(clampI32(t.Max))
		}
	case api.ParserLong:
		writeBounds(w, t)
		if t.HasMin {
			w.I64(int64(t.Min))
		}
		if t.HasMax {
			w.I64(int64(t.Max))
		}
	case api.ParserString:
		w.VarInt(int32(t.StringMode))
	case api.ParserEntity:
		var f byte
		if t.Single {
			f |= 0x01
		}
		if t.PlayersOnly {
			f |= 0x02
		}
		w.U8(f)
	case ParserScoreHolder:
		var f byte
		if !t.Single {
			f |= 0x01
		}
		w.U8(f)
	case api.ParserTime:
		var m int32
		if t.HasMin {
			m = clampI32(t.Min)
		}
		w.I32(m)
	case ParserResourceOrTag, ParserResourceOrKey, ParserResourceFull, api.ParserResourceKey, ParserResourceSelect:
		w.String(string(t.Registry.Normalize()))
	}
}

func writeBounds(w *proto.Writer, t api.ArgumentType) {
	var f byte
	if t.HasMin {
		f |= 0x01
	}
	if t.HasMax {
		f |= 0x02
	}
	w.U8(f)
}

func clampI32(v float64) int32 {
	return int32(max(math.MinInt32, min(math.MaxInt32, v)))
}
