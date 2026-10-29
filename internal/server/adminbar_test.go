package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/metrics"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// wire assembles expected packet bodies straight from the layouts read with
// javap from the 26.3 server classes, independent of the generated codecs.
type wire struct{ b []byte }

func (w *wire) varint(v int32) *wire {
	u := uint32(v)
	for u >= 0x80 {
		w.b = append(w.b, byte(u)|0x80)
		u >>= 7
	}
	w.b = append(w.b, byte(u))
	return w
}

// utf is FriendlyByteBuf.writeUtf: VarInt byte length + UTF-8.
func (w *wire) utf(s string) *wire { w.varint(int32(len(s))); w.b = append(w.b, s...); return w }
func (w *wire) u8(v byte) *wire    { w.b = append(w.b, v); return w }
func (w *wire) f32(v float32) *wire {
	w.b = binary.BigEndian.AppendUint32(w.b, math.Float32bits(v))
	return w
}
func (w *wire) uuid(id [16]byte) *wire { w.b = append(w.b, id[:]...); return w }

// component is ComponentSerialization.TRUSTED_STREAM_CODEC: a nameless
// network NBT tag. Only the shapes the admin bar emits are covered.
func (w *wire) component(c api.Component) *wire {
	if c.Color == "" && c.Bold == nil && len(c.Extra) == 0 {
		w.u8(8)
		w.nbtStr(c.Text)
		return w
	}
	w.u8(10)
	w.compound(c)
	return w
}

func (w *wire) nbtStr(s string) {
	w.b = binary.BigEndian.AppendUint16(w.b, uint16(len(s)))
	w.b = append(w.b, s...)
}

func (w *wire) compound(c api.Component) {
	w.u8(8)
	w.nbtStr("text")
	w.nbtStr(c.Text)
	if c.Color != "" {
		w.u8(8)
		w.nbtStr("color")
		w.nbtStr(c.Color)
	}
	if c.Bold != nil {
		w.u8(1)
		w.nbtStr("bold")
		if *c.Bold {
			w.u8(1)
		} else {
			w.u8(0)
		}
	}
	if len(c.Extra) > 0 {
		w.u8(9)
		w.nbtStr("extra")
		w.u8(10)
		w.b = binary.BigEndian.AppendUint32(w.b, uint32(len(c.Extra)))
		for _, e := range c.Extra {
			w.compound(e)
		}
	}
	w.u8(0)
}

// Clientbound play IDs: registration order in GameProtocols.
const (
	idBossEvent           = 0x09
	idResetScore          = 0x50
	idSetDisplayObjective = 0x64
	idSetObjective        = 0x6C
	idSetScore            = 0x70
)

func newBareServer(t testing.TB) *Server {
	t.Helper()
	s, err := New(Config{Dir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testStats() *api.ServerStats {
	return &api.ServerStats{
		Uptime: 95 * time.Second, Version: EngineVersion, GameVersion: packets.Version, Protocol: packets.Protocol,
		TPS: 20, TPS1m: 20, TPS5m: 19.9, TPS15m: 19.8, MSPT: 3.2, MSPTP95: 5.1, MSPTMax: 9.8,
		CPU: 18, NumCPU: 4, GOMAXPROCS: 4, CPUCores: []float64{10, 40, 70, 95},
		HeapBytes: 412 << 20, RSSBytes: 530 << 20, SysBytes: 612 << 20, GCCount: 12, Goroutines: 40,
		TickWorkers: 4, WorkerBusy: 0.25, Parallelism: 1, Regions: 3, IOWorkers: 2,
		LoadedChunks: 81, Players: 2, Entities: 5, MaxPlayers: 20, NetIn: 2048, NetOut: 4096,
	}
}

func checkBytes(t *testing.T, what string, got []byte, want *wire) {
	t.Helper()
	if !bytes.Equal(got, want.b) {
		t.Fatalf("%s:\n got %x\nwant %x", what, got, want.b)
	}
}

func TestAdminBarWireBytes(t *testing.T) {
	s := newBareServer(t)
	b := s.bar

	// set_objective: utf name, byte method; ADD/CHANGE add component,
	// writeEnum(renderType), Optional<NumberFormat> as bool + registry VarInt.
	checkBytes(t, "set_objective add", b.objAdd, new(wire).varint(idSetObjective).utf("kremenmc_admin").u8(0).
		component(metrics.Title()).varint(0).u8(1).varint(0))
	checkBytes(t, "set_objective remove", b.objRemove, new(wire).varint(idSetObjective).utf("kremenmc_admin").u8(1))
	// set_display_objective: writeById(DisplaySlot::id) VarInt, utf name.
	checkBytes(t, "set_display_objective", b.display, new(wire).varint(idSetDisplayObjective).varint(1).utf("kremenmc_admin"))

	// set_score: utf owner, utf objective, VarInt score, Optional<Component>,
	// Optional<NumberFormat> (absent: the objective's blank format applies).
	st := testStats()
	lines := metrics.Lines(st)
	raws := b.renderSidebar(st)
	if len(raws) != len(lines) || len(lines) > metrics.MaxLines {
		t.Fatalf("%d packets for %d lines", len(raws), len(lines))
	}
	for i, l := range lines {
		checkBytes(t, "set_score "+strconv.Itoa(i), raws[i], new(wire).varint(idSetScore).utf(barOwners[i]).utf("kremenmc_admin").
			varint(int32(metrics.MaxLines-i)).u8(1).component(l).u8(0))
	}
	if again := b.renderSidebar(st); len(again) != 0 {
		t.Fatalf("unchanged stats resent %d lines", len(again))
	}
	st.Players = 3
	diff := b.renderSidebar(st)
	if len(diff) != 1 || !bytes.Contains(diff[0], []byte("3/20")) {
		t.Fatalf("one changed line produced %d packets", len(diff))
	}
	if !bytes.Equal(b.linePkt[9], diff[0]) {
		t.Fatal("full-state cache not updated with the diff")
	}

	// reset_score: utf owner, writeNullable(objective) = bool + utf.
	n := len(b.lineNBT)
	b.lineNBT = append(b.lineNBT, []byte{1}, []byte{2})
	b.linePkt = append(b.linePkt, nil, nil)
	resets := b.renderSidebar(st)
	if len(resets) != 2 || len(b.lineNBT) != n || len(b.linePkt) != n {
		t.Fatalf("%d resets, %d lines cached", len(resets), len(b.lineNBT))
	}
	for i, r := range resets {
		checkBytes(t, "reset_score", r, new(wire).varint(idResetScore).utf(barOwners[n+i]).u8(1).utf("kremenmc_admin"))
	}

	// boss_event: UUID, writeEnum(operation), then the operation's fields.
	title := metrics.BossTitle(st)
	b.boss = bossState{title: toText(title), progress: 0.75, color: bossYellow}
	checkBytes(t, "boss add", b.bossAddPacket(), new(wire).varint(idBossEvent).uuid(b.bossID).varint(0).
		component(title).f32(0.75).varint(4).varint(0).u8(0))
	checkBytes(t, "boss remove", b.bossRm, new(wire).varint(idBossEvent).uuid(b.bossID).varint(1))

	b.bossOK = false
	nb := b.renderBoss()
	if nb.color != bossGreen || nb.progress != 1 {
		t.Fatalf("idle server boss state %+v", nb)
	}
	nb.progress, nb.color = 0.5, bossRed
	ups := b.bossUpdates(nb)
	if len(ups) != 3 {
		t.Fatalf("%d boss updates", len(ups))
	}
	checkBytes(t, "boss name", ups[0], new(wire).varint(idBossEvent).uuid(b.bossID).varint(3).component(metrics.BossTitle(func() *api.ServerStats {
		st := *s.stats.latest()
		st.TPS, st.MSPT = s.TPS(), s.MSPT()
		return &st
	}())))
	checkBytes(t, "boss progress", ups[1], new(wire).varint(idBossEvent).uuid(b.bossID).varint(2).f32(0.5))
	checkBytes(t, "boss style", ups[2], new(wire).varint(idBossEvent).uuid(b.bossID).varint(4).varint(2).varint(0))
	if again := b.bossUpdates(nb); len(again) != 0 {
		t.Fatalf("unchanged boss resent %d ops", len(again))
	}
}

func readPrefs(t *testing.T, s *Server) map[string]barMode {
	t.Helper()
	m := map[string]barMode{}
	raw, err := os.ReadFile(filepath.Join(s.dir, "adminbar.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func viewerCount(s *Server) int {
	s.bar.mu.Lock()
	defer s.bar.mu.Unlock()
	return len(s.bar.viewers)
}

func TestAdminBarToggle(t *testing.T) {
	s := startServer(t, nil)
	id := mcnet.OfflineUUID("Admin")
	key := api.UUID(id).String()
	s.setOp(id, "Admin", true)
	c := dial(t, s.Addr(), "Admin")
	c.joinPlay(s.Addr())

	c.send(&packets.ServerboundChatCommand{Command: "adminbar"})
	obj := expect[*packets.ClientboundSetObjective](c, nil)
	if obj.Name != barObjective || obj.Action != 0 || obj.Type != 0 || !obj.NumberFormat.Ok || obj.NumberFormat.V != 0 {
		t.Fatalf("objective %+v", obj)
	}
	for i := range metrics.Lines(testStats()) {
		sc := expect[*packets.ClientboundSetScore](c, nil)
		if sc.ItemName != barOwners[i] || sc.ScoreName != barObjective || !sc.DisplayName.Ok || sc.NumberFormat.Ok {
			t.Fatalf("line %d: %+v", i, sc)
		}
	}
	if d := expect[*packets.ClientboundSetDisplayObjective](c, nil); d.Position != 1 || d.Name != barObjective {
		t.Fatalf("display %+v", d)
	}
	if be := expect[*packets.ClientboundBossEvent](c, nil); be.Action != 0 || be.Color != bossGreen || be.Health != 1 {
		t.Fatalf("boss add %+v", be)
	}
	expect[*packets.ClientboundSystemChat](c, func(m *packets.ClientboundSystemChat) bool {
		return fromText(m.Content).Plain() == "Admin bar: sidebar on, boss bar on"
	})
	if m := readPrefs(t, s); m[key] != (barMode{Sidebar: true, BossBar: true}) {
		t.Fatalf("saved %+v", m)
	}

	// the uptime line changes every second; only changed lines are resent
	up := expect[*packets.ClientboundSetScore](c, func(p *packets.ClientboundSetScore) bool {
		return p.ItemName == barOwners[len(metrics.Lines(testStats()))-1]
	})
	if !strings.Contains(up.DisplayName.V.String(), "26.3 (777)") {
		t.Fatalf("uptime line %q", up.DisplayName.V.String())
	}

	c.send(&packets.ServerboundChatCommand{Command: "kremenmc stats"})
	expect[*packets.ClientboundSystemChat](c, func(m *packets.ClientboundSystemChat) bool {
		return strings.HasPrefix(fromText(m.Content).Plain(), "▍TPS ")
	})

	c.send(&packets.ServerboundChatCommand{Command: "adminbar sidebar"})
	if o := expect[*packets.ClientboundSetObjective](c, nil); o.Action != 1 || o.Name != barObjective {
		t.Fatalf("sidebar off %+v", o)
	}
	if m := readPrefs(t, s); m[key] != (barMode{BossBar: true}) {
		t.Fatalf("saved %+v", m)
	}
	c.send(&packets.ServerboundChatCommand{Command: "adminbar off"})
	if be := expect[*packets.ClientboundBossEvent](c, func(p *packets.ClientboundBossEvent) bool { return p.Action == 1 }); be.EntityUUID != s.bar.bossID {
		t.Fatalf("boss remove %+v", be)
	}
	if m := readPrefs(t, s); len(m) != 0 || viewerCount(s) != 0 {
		t.Fatalf("saved %+v, %d viewers", m, viewerCount(s))
	}

	c.send(&packets.ServerboundChatCommand{Command: "adminbar on"})
	expect[*packets.ClientboundSetObjective](c, func(p *packets.ClientboundSetObjective) bool { return p.Action == 0 })
	expect[*packets.ClientboundBossEvent](c, func(p *packets.ClientboundBossEvent) bool { return p.Action == 0 })
	c.nc.Close()
	waitFor(t, func() bool { return len(s.Players()) == 0 })
	if n := viewerCount(s); n != 0 {
		t.Fatalf("%d viewers after quit", n)
	}

	// restored during join, before the first chunk batch
	c = dial(t, s.Addr(), "Admin")
	c.join(s.Addr())
	expect[*packets.ClientboundSetObjective](c, func(p *packets.ClientboundSetObjective) bool { return p.Action == 0 })
	expect[*packets.ClientboundBossEvent](c, func(p *packets.ClientboundBossEvent) bool { return p.Action == 0 })

	s.setOp(id, "Admin", false)
	expect[*packets.ClientboundSetObjective](c, func(p *packets.ClientboundSetObjective) bool { return p.Action == 1 })
	expect[*packets.ClientboundBossEvent](c, func(p *packets.ClientboundBossEvent) bool { return p.Action == 1 })
	if m := readPrefs(t, s); m[key] != (barMode{Sidebar: true, BossBar: true}) || viewerCount(s) != 0 {
		t.Fatalf("de-op: saved %+v, %d viewers", m, viewerCount(s))
	}
}

func TestAdminBarDenied(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Guest")
	c.joinPlay(s.Addr())
	c.send(&packets.ServerboundChatCommand{Command: "adminbar on"})
	expect[*packets.ClientboundSystemChat](c, nil)
	if n := viewerCount(s); n != 0 {
		t.Fatalf("%d viewers", n)
	}
}

func TestStatsSample(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Stats")
	c.joinPlay(s.Addr())
	var st api.ServerStats
	waitFor(t, func() bool {
		st = s.Stats()
		return st.Players == 1 && st.NetIn > 0 && st.NetOut > 0 && st.Uptime > time.Second
	})
	switch {
	case st.GameVersion != packets.Version || st.Protocol != packets.Protocol || st.Version != EngineVersion:
		t.Fatalf("version %+v", st)
	case st.TPS <= 0 || st.TPS1m <= 0 || st.MSPT <= 0 || st.MSPTMax < st.MSPTP95 || st.MSPTP95 < st.MSPT*0.1:
		t.Fatalf("tick stats tps=%v/%v mspt=%v p95=%v max=%v", st.TPS, st.TPS1m, st.MSPT, st.MSPTP95, st.MSPTMax)
	case st.TickWorkers <= 0 || st.Regions <= 0 || st.IOWorkers <= 0 || st.LoadedChunks <= 0 || st.Entities < 1:
		t.Fatalf("world stats %+v", st)
	case st.PacketsIn <= 0 || st.PacketsOut <= 0 || st.HeapBytes == 0 || st.Goroutines == 0:
		t.Fatalf("net/runtime stats %+v", st)
	}
}

// BenchmarkAdminBarBuild is one second of server time with every line and
// boss field changing: one sample, one sidebar render, two boss updates.
func BenchmarkAdminBarBuild(b *testing.B) {
	s := newBareServer(b)
	ss := newStatsSampler(s)
	bar := s.bar
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(time.Second)
		st := ss.sample(now)
		bar.lineNBT, bar.linePkt = bar.lineNBT[:0], bar.linePkt[:0]
		bar.renderSidebar(st)
		for range 2 {
			bar.bossOK = false
			bar.bossUpdates(bar.renderBoss())
		}
	}
}

// BenchmarkAdminBar100Viewers does the same against 100 connected clients
// that all show both bars, so each op also queues the packets per viewer.
func BenchmarkAdminBar100Viewers(b *testing.B) {
	const n = 100
	s := startServer(b, func(p *config.ServerProperties) { p.ViewDistance = 2; p.SimulationDistance = 2 })
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			c := dial(b, s.Addr(), "bar"+strconv.Itoa(i))
			c.view = 2
			c.joinPlay(s.Addr())
			go func() {
				for range c.in {
				}
			}()
		}()
	}
	wg.Wait()
	for _, p := range s.Players() {
		if !s.SetAdminBar(p, true, true) {
			b.Fatal("toggle failed")
		}
	}
	if v := viewerCount(s); v != n {
		b.Fatalf("%d viewers", v)
	}
	ss := newStatsSampler(s)
	bar := s.bar
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now = now.Add(time.Second)
		bar.mu.Lock()
		bar.lineNBT, bar.linePkt, bar.bossOK = bar.lineNBT[:0], bar.linePkt[:0], false
		bar.mu.Unlock()
		bar.refresh(ss.sample(now))
		for range 2 {
			bar.mu.Lock()
			bar.bossOK = false
			bar.mu.Unlock()
			bar.updateBoss()
		}
		// let the writers drain, as the real 1 Hz cadence would, so client
		// queues never overflow and every send stays a real enqueue
		b.StopTimer()
		time.Sleep(2 * time.Millisecond)
		b.StartTimer()
	}
	b.StopTimer()
	if got := len(s.Players()); got != n {
		b.Fatalf("%d of %d viewers still online", got, n)
	}
}
