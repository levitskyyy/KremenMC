package kremenmc.bridge;

import java.util.ArrayList;
import java.util.Collection;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.format.NamedTextColor;
import org.bukkit.ChatColor;
import org.bukkit.OfflinePlayer;
import org.bukkit.entity.Entity;
import org.bukkit.entity.Player;
import org.bukkit.scoreboard.Criteria;
import org.bukkit.scoreboard.DisplaySlot;
import org.bukkit.scoreboard.Objective;
import org.bukkit.scoreboard.RenderType;
import org.bukkit.scoreboard.Score;
import org.bukkit.scoreboard.Scoreboard;
import org.bukkit.scoreboard.ScoreboardManager;
import org.bukkit.scoreboard.Team;

/**
 * Bukkit scoreboards kept on the Java side. Viewers of a board get its state
 * as set_objective / set_score / set_player_team packets through Go.
 */
final class Scoreboards {
    private static volatile Scoreboards instance;

    static Scoreboards of(Bridge b) {
        Scoreboards s = instance;
        if (s == null) {
            synchronized (Scoreboards.class) {
                s = instance;
                if (s == null) instance = s = new Scoreboards(b);
            }
        }
        return s;
    }

    private static final List<String> COLORS = List.of("black", "dark_blue", "dark_green", "dark_aqua", "dark_red",
            "dark_purple", "gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple", "yellow", "white");

    final Bridge b;
    final Board main;
    final ScoreboardManager manager;
    private final Map<UUID, Board> boards = new ConcurrentHashMap<>();

    private Scoreboards(Bridge b) {
        this.b = b;
        this.main = new Board(this);
        this.manager = Stubs.proxy(ScoreboardManager.class, new Manager());
    }

    public final class Manager {
        public Scoreboard getMainScoreboard() { return main.self; }
        public Scoreboard getNewScoreboard() { return new Board(Scoreboards.this).self; }
    }

    Scoreboard boardOf(UUID player) { return boards.getOrDefault(player, main).self; }

    void setBoard(UUID player, Scoreboard s) {
        if (!(Stubs.implOf(s) instanceof Board nb)) throw new IllegalArgumentException("not a server scoreboard");
        Board old = boards.getOrDefault(player, main);
        if (old == nb) return;
        synchronized (this) {
            old.clearFor(player);
            if (nb == main) boards.remove(player);
            else boards.put(player, nb);
            nb.sendTo(player);
        }
    }

    void onJoin(UUID player) {
        boards.remove(player);
        main.sendTo(player);
    }

    void onQuit(UUID player) { boards.remove(player); }

    List<UUID> viewers(Board bd) {
        List<UUID> out = new ArrayList<>();
        for (Player p : b.players.online()) {
            UUID id = p.getUniqueId();
            if (boards.getOrDefault(id, main) == bd) out.add(id);
        }
        return out;
    }

    void packet(UUID to, String kind, Map<String, Object> f) { b.conn.notify(Wire.OP_SEND_PACKET, to, kind, f); }

    static int slotId(DisplaySlot s) {
        String n = s.name();
        if (n.equals("PLAYER_LIST")) return 0;
        if (n.equals("SIDEBAR")) return 1;
        if (n.equals("BELOW_NAME")) return 2;
        if (n.startsWith("SIDEBAR_TEAM_")) return 3 + Math.max(0, COLORS.indexOf(n.substring(13).toLowerCase(Locale.ROOT)));
        return 1;
    }

    static int colorId(NamedTextColor c) {
        if (c == null) return 21;
        int i = COLORS.indexOf(c.toString());
        return i < 0 ? 21 : i;
    }

    public static final class Board {
        final Scoreboards sb;
        final Scoreboard self;
        final Map<String, Obj> objectives = new LinkedHashMap<>();
        final Map<String, TeamImpl> teams = new LinkedHashMap<>();
        final Map<Integer, Obj> display = new HashMap<>();

        Board(Scoreboards sb) {
            this.sb = sb;
            this.self = Stubs.proxy(Scoreboard.class, this);
        }

        void broadcast(String kind, Map<String, Object> f) {
            for (UUID v : sb.viewers(this)) sb.packet(v, kind, f);
        }

        synchronized void sendTo(UUID p) {
            for (Obj o : objectives.values()) {
                sb.packet(p, "set_objective", o.fields(0));
                for (Map.Entry<String, Integer> e : o.scores.entrySet()) sb.packet(p, "set_score", o.scoreFields(e.getKey(), e.getValue()));
            }
            for (Map.Entry<Integer, Obj> e : display.entrySet()) sb.packet(p, "set_display_objective", Bridge.fields("slot", e.getKey(), "name", e.getValue().name));
            for (TeamImpl t : teams.values()) sb.packet(p, "set_player_team", t.fields(0, new ArrayList<>(t.entries)));
        }

        synchronized void clearFor(UUID p) {
            for (Obj o : objectives.values()) sb.packet(p, "set_objective", Bridge.fields("name", o.name, "method", 1));
            for (TeamImpl t : teams.values()) sb.packet(p, "set_player_team", Bridge.fields("name", t.name, "method", 1, "players", List.of()));
        }

        public synchronized Objective registerNewObjective(String name, String criteria) {
            return registerNewObjective(name, criteria, Component.text(name), RenderType.INTEGER);
        }

        public Objective registerNewObjective(String name, String criteria, String displayName) {
            return registerNewObjective(name, criteria, leg(displayName), RenderType.INTEGER);
        }

        public Objective registerNewObjective(String name, String criteria, String displayName, RenderType rt) {
            return registerNewObjective(name, criteria, leg(displayName), rt);
        }

        public Objective registerNewObjective(String name, String criteria, Component displayName) {
            return registerNewObjective(name, criteria, displayName, RenderType.INTEGER);
        }

        public synchronized Objective registerNewObjective(String name, String criteria, Component displayName, RenderType rt) {
            if (objectives.containsKey(name)) throw new IllegalArgumentException("An objective of name '" + name + "' already exists");
            Obj o = new Obj(this, name, criteria, displayName == null ? Component.text(name) : displayName, rt == null ? RenderType.INTEGER : rt);
            objectives.put(name, o);
            broadcast("set_objective", o.fields(0));
            return o.self;
        }

        public Objective registerNewObjective(String name, Criteria c, Component displayName) { return registerNewObjective(name, c.getName(), displayName, c.getDefaultRenderType()); }
        public Objective registerNewObjective(String name, Criteria c, Component displayName, RenderType rt) { return registerNewObjective(name, c.getName(), displayName, rt); }
        public Objective registerNewObjective(String name, Criteria c, String displayName) { return registerNewObjective(name, c.getName(), leg(displayName), c.getDefaultRenderType()); }
        public Objective registerNewObjective(String name, Criteria c, String displayName, RenderType rt) { return registerNewObjective(name, c.getName(), leg(displayName), rt); }

        public synchronized Objective getObjective(String name) {
            Obj o = objectives.get(name);
            return o == null ? null : o.self;
        }

        public synchronized Objective getObjective(DisplaySlot slot) {
            Obj o = display.get(slotId(slot));
            return o == null ? null : o.self;
        }

        public synchronized Set<Objective> getObjectives() {
            Set<Objective> out = new LinkedHashSet<>();
            for (Obj o : objectives.values()) out.add(o.self);
            return out;
        }

        public synchronized Set<Objective> getObjectivesByCriteria(String c) {
            Set<Objective> out = new LinkedHashSet<>();
            for (Obj o : objectives.values()) if (o.criteria.equals(c)) out.add(o.self);
            return out;
        }

        public Set<Objective> getObjectivesByCriteria(Criteria c) { return getObjectivesByCriteria(c.getName()); }

        public synchronized Set<Score> getScores(String entry) {
            Set<Score> out = new HashSet<>();
            for (Obj o : objectives.values()) if (o.scores.containsKey(entry)) out.add(o.score(entry));
            return out;
        }

        public Set<Score> getScores(OfflinePlayer p) { return getScores(p.getName()); }
        public Set<Score> getScoresFor(Entity e) { return getScores(entry(e)); }

        public synchronized void resetScores(String entry) {
            for (Obj o : objectives.values()) o.scores.remove(entry);
            broadcast("reset_score", Bridge.fields("owner", entry));
        }

        public void resetScores(OfflinePlayer p) { resetScores(p.getName()); }
        public void resetScoresFor(Entity e) { resetScores(entry(e)); }

        public synchronized Set<String> getEntries() {
            Set<String> out = new LinkedHashSet<>();
            for (Obj o : objectives.values()) out.addAll(o.scores.keySet());
            return out;
        }

        public synchronized void clearSlot(DisplaySlot slot) {
            if (display.remove(slotId(slot)) != null) broadcast("set_display_objective", Bridge.fields("slot", slotId(slot), "name", ""));
        }

        public synchronized Team registerNewTeam(String name) {
            if (teams.containsKey(name)) throw new IllegalArgumentException("Team name '" + name + "' is already in use");
            TeamImpl t = new TeamImpl(this, name);
            teams.put(name, t);
            broadcast("set_player_team", t.fields(0, List.of()));
            return t.self;
        }

        public synchronized Team getTeam(String name) {
            TeamImpl t = teams.get(name);
            return t == null ? null : t.self;
        }

        public synchronized Set<Team> getTeams() {
            Set<Team> out = new LinkedHashSet<>();
            for (TeamImpl t : teams.values()) out.add(t.self);
            return out;
        }

        public synchronized Team getEntryTeam(String entry) {
            for (TeamImpl t : teams.values()) if (t.entries.contains(entry)) return t.self;
            return null;
        }

        public Team getPlayerTeam(OfflinePlayer p) { return getEntryTeam(p.getName()); }
        public Team getEntityTeam(Entity e) { return getEntryTeam(entry(e)); }
        public Set<OfflinePlayer> getPlayers() { return new HashSet<>(); }

        @Override public String toString() { return "CraftScoreboard"; }
    }

    static Component leg(String s) {
        return net.kyori.adventure.text.serializer.legacy.LegacyComponentSerializer.legacySection().deserialize(s == null ? "" : s);
    }

    static String entry(Entity e) { return e instanceof Player p ? p.getName() : e.getUniqueId().toString(); }

    public static final class Obj {
        final Board board;
        final Objective self;
        final String name, criteria;
        volatile Component displayName;
        volatile RenderType renderType;
        volatile boolean autoUpdate = true;
        final Map<String, Integer> scores = new LinkedHashMap<>();
        final Map<String, Component> customNames = new HashMap<>();
        boolean registered = true;

        Obj(Board board, String name, String criteria, Component displayName, RenderType rt) {
            this.board = board;
            this.name = name;
            this.criteria = criteria;
            this.displayName = displayName;
            this.renderType = rt;
            this.self = Stubs.proxy(Objective.class, this);
        }

        Map<String, Object> fields(int method) {
            return Bridge.fields("name", name, "method", method, "displayName", Text.json(displayName),
                    "renderType", renderType == RenderType.HEARTS ? 1 : 0, "numberFormat", null);
        }

        Map<String, Object> scoreFields(String entry, int v) {
            Component cn = customNames.get(entry);
            Map<String, Object> f = Bridge.fields("owner", entry, "objective", name, "score", v, "numberFormat", null);
            if (cn != null) f.put("displayName", Text.json(cn));
            return f;
        }

        private void check() {
            if (!registered) throw new IllegalStateException("Unregistered scoreboard component");
        }

        public String getName() { return name; }
        public String getCriteria() { return criteria; }
        public Criteria getTrackedCriteria() { return criteria(criteria); }
        public boolean isModifiable() { return true; }
        public Scoreboard getScoreboard() { return registered ? board.self : null; }
        public Component displayName() { return displayName; }
        public String getDisplayName() { return Text.legacy(displayName); }
        public void displayName(Component c) { setDisplay(c == null ? Component.empty() : c); }
        public void setDisplayName(String s) { setDisplay(leg(s)); }
        public RenderType getRenderType() { return renderType; }
        public boolean willAutoUpdateDisplay() { return autoUpdate; }
        public void setAutoUpdateDisplay(boolean v) { autoUpdate = v; }

        public void setRenderType(RenderType rt) {
            synchronized (board) {
                check();
                renderType = rt;
                board.broadcast("set_objective", fields(2));
            }
        }

        private void setDisplay(Component c) {
            synchronized (board) {
                check();
                displayName = c;
                board.broadcast("set_objective", fields(2));
            }
        }

        public void unregister() {
            synchronized (board) {
                check();
                registered = false;
                board.objectives.remove(name);
                board.display.values().removeIf(o -> o == this);
                board.broadcast("set_objective", Bridge.fields("name", name, "method", 1));
            }
        }

        public void setDisplaySlot(DisplaySlot slot) {
            synchronized (board) {
                check();
                board.display.values().removeIf(o -> o == this);
                if (slot == null) return;
                board.display.put(slotId(slot), this);
                board.broadcast("set_display_objective", Bridge.fields("slot", slotId(slot), "name", name));
            }
        }

        public DisplaySlot getDisplaySlot() {
            synchronized (board) {
                for (Map.Entry<Integer, Obj> e : board.display.entrySet()) {
                    if (e.getValue() != this) continue;
                    for (DisplaySlot s : DisplaySlot.values()) if (slotId(s) == e.getKey()) return s;
                }
                return null;
            }
        }

        Score score(String entry) { return Stubs.proxy(Score.class, new ScoreImpl(this, entry)); }
        public Score getScore(String entry) { check(); return score(entry); }
        public Score getScore(OfflinePlayer p) { return getScore(p.getName()); }
        public Score getScoreFor(Entity e) { return getScore(entry(e)); }

        @Override public String toString() { return "CraftObjective{" + name + "}"; }
    }

    public static final class ScoreImpl {
        final Obj obj;
        final String entry;

        ScoreImpl(Obj obj, String entry) {
            this.obj = obj;
            this.entry = entry;
        }

        public String getEntry() { return entry; }
        public Objective getObjective() { return obj.self; }
        public Scoreboard getScoreboard() { return obj.getScoreboard(); }
        public OfflinePlayer getPlayer() { return org.bukkit.Bukkit.getOfflinePlayer(entry); }

        public int getScore() {
            synchronized (obj.board) {
                Integer v = obj.scores.get(entry);
                return v == null ? 0 : v;
            }
        }

        public boolean isScoreSet() {
            synchronized (obj.board) { return obj.scores.containsKey(entry); }
        }

        public void setScore(int v) {
            synchronized (obj.board) {
                obj.check();
                obj.scores.put(entry, v);
                obj.board.broadcast("set_score", obj.scoreFields(entry, v));
            }
        }

        public void resetScore() {
            synchronized (obj.board) {
                if (obj.scores.remove(entry) != null) obj.board.broadcast("reset_score", Bridge.fields("owner", entry, "objective", obj.name));
            }
        }

        public Component customName() {
            synchronized (obj.board) { return obj.customNames.get(entry); }
        }

        public void customName(Component c) {
            synchronized (obj.board) {
                if (c == null) obj.customNames.remove(entry);
                else obj.customNames.put(entry, c);
                obj.board.broadcast("set_score", obj.scoreFields(entry, getScore()));
            }
        }

        public boolean isTriggerable() { return obj.criteria.equals("trigger"); }
        public void setTriggerable(boolean v) {}

        @Override public boolean equals(Object o) { return o instanceof ScoreImpl s && s.obj == obj && s.entry.equals(entry); }
        @Override public int hashCode() { return obj.hashCode() * 31 + entry.hashCode(); }
    }

    public static final class TeamImpl {
        final Board board;
        final Team self;
        final String name;
        final Set<String> entries = new LinkedHashSet<>();
        Component displayName, prefix = Component.empty(), suffix = Component.empty();
        NamedTextColor color;
        boolean friendlyFire = true, seeInvisible = true;
        final Map<Team.Option, Team.OptionStatus> options = new HashMap<>();
        boolean registered = true;

        TeamImpl(Board board, String name) {
            this.board = board;
            this.name = name;
            this.displayName = Component.text(name);
            this.self = Stubs.proxy(Team.class, this);
        }

        Map<String, Object> fields(int method, List<String> players) {
            Map<String, Object> f = Bridge.fields("name", name, "method", method, "players", players);
            if (method == 0 || method == 2) {
                int opts = (friendlyFire ? 1 : 0) | (seeInvisible ? 2 : 0);
                f.put("params", Bridge.fields("displayName", Text.json(displayName), "prefix", Text.json(prefix), "suffix", Text.json(suffix),
                        "visibility", visibility(options.get(Team.Option.NAME_TAG_VISIBILITY)),
                        "collision", collision(options.get(Team.Option.COLLISION_RULE)), "color", colorId(color), "options", opts));
            }
            return f;
        }

        static int visibility(Team.OptionStatus s) {
            if (s == null) return 0;
            return switch (s) {
                case ALWAYS -> 0;
                case NEVER -> 1;
                case FOR_OWN_TEAM -> 2;
                case FOR_OTHER_TEAMS -> 3;
            };
        }

        static int collision(Team.OptionStatus s) {
            if (s == null) return 0;
            return switch (s) {
                case ALWAYS -> 0;
                case NEVER -> 1;
                case FOR_OTHER_TEAMS -> 2;
                case FOR_OWN_TEAM -> 3;
            };
        }

        private void check() {
            if (!registered) throw new IllegalStateException("Unregistered scoreboard component");
        }

        private void update() {
            synchronized (board) {
                check();
                board.broadcast("set_player_team", fields(2, List.of()));
            }
        }

        public String getName() { return name; }
        public Scoreboard getScoreboard() { return registered ? board.self : null; }
        public Component displayName() { return displayName; }
        public void displayName(Component c) { displayName = c == null ? Component.empty() : c; update(); }
        public String getDisplayName() { return Text.legacy(displayName); }
        public void setDisplayName(String s) { displayName(leg(s)); }
        public Component prefix() { return prefix; }
        public void prefix(Component c) { prefix = c == null ? Component.empty() : c; update(); }
        public String getPrefix() { return Text.legacy(prefix); }
        public void setPrefix(String s) { prefix(leg(s)); }
        public Component suffix() { return suffix; }
        public void suffix(Component c) { suffix = c == null ? Component.empty() : c; update(); }
        public String getSuffix() { return Text.legacy(suffix); }
        public void setSuffix(String s) { suffix(leg(s)); }
        public boolean hasColor() { return color != null; }
        public net.kyori.adventure.text.format.TextColor color() { return color; }
        public void color(NamedTextColor c) { color = c; update(); }

        public ChatColor getColor() {
            if (color == null) return ChatColor.RESET;
            ChatColor c = ChatColor.valueOf(color.toString().toUpperCase(Locale.ROOT));
            return c;
        }

        public void setColor(ChatColor c) {
            color = c == null || !c.isColor() ? null : NamedTextColor.NAMES.value(c.name().toLowerCase(Locale.ROOT));
            update();
        }

        public boolean allowFriendlyFire() { return friendlyFire; }
        public void setAllowFriendlyFire(boolean v) { friendlyFire = v; update(); }
        public boolean canSeeFriendlyInvisibles() { return seeInvisible; }
        public void setCanSeeFriendlyInvisibles(boolean v) { seeInvisible = v; update(); }
        public Team.OptionStatus getOption(Team.Option o) { return options.getOrDefault(o, Team.OptionStatus.ALWAYS); }
        public void setOption(Team.Option o, Team.OptionStatus s) { options.put(o, s); update(); }
        public org.bukkit.scoreboard.NameTagVisibility getNameTagVisibility() { return org.bukkit.scoreboard.NameTagVisibility.ALWAYS; }

        public void addEntry(String e) { addEntries(List.of(e)); }
        public void addEntries(String... es) { addEntries(List.of(es)); }

        public void addEntries(Collection<String> es) {
            synchronized (board) {
                check();
                List<String> added = new ArrayList<>();
                for (String e : es) {
                    for (TeamImpl t : board.teams.values()) if (t != this) t.entries.remove(e);
                    if (entries.add(e)) added.add(e);
                }
                if (!added.isEmpty()) board.broadcast("set_player_team", fields(3, added));
            }
        }

        public boolean removeEntry(String e) { return removeEntries(List.of(e)); }
        public boolean removeEntries(String... es) { return removeEntries(List.of(es)); }

        public boolean removeEntries(Collection<String> es) {
            synchronized (board) {
                check();
                List<String> gone = new ArrayList<>();
                for (String e : es) if (entries.remove(e)) gone.add(e);
                if (!gone.isEmpty()) board.broadcast("set_player_team", fields(4, gone));
                return !gone.isEmpty();
            }
        }

        public void addEntity(Entity e) { addEntry(entry(e)); }
        public void addEntities(Entity... es) { for (Entity e : es) addEntity(e); }
        public void addEntities(Collection<Entity> es) { for (Entity e : es) addEntity(e); }
        public boolean removeEntity(Entity e) { return removeEntry(entry(e)); }
        public boolean hasEntity(Entity e) { return hasEntry(entry(e)); }
        public void addPlayer(OfflinePlayer p) { addEntry(p.getName()); }
        public boolean removePlayer(OfflinePlayer p) { return removeEntry(p.getName()); }
        public boolean hasPlayer(OfflinePlayer p) { return hasEntry(p.getName()); }

        public Set<OfflinePlayer> getPlayers() {
            Set<OfflinePlayer> out = new HashSet<>();
            for (String e : getEntries()) {
                Player p = board.sb.b.players.byName(e, true);
                if (p != null) out.add(p);
            }
            return out;
        }

        public boolean hasEntry(String e) {
            synchronized (board) { return entries.contains(e); }
        }

        public Set<String> getEntries() {
            synchronized (board) { return new LinkedHashSet<>(entries); }
        }

        public int getSize() {
            synchronized (board) { return entries.size(); }
        }

        public void unregister() {
            synchronized (board) {
                check();
                registered = false;
                board.teams.remove(name);
                board.broadcast("set_player_team", Bridge.fields("name", name, "method", 1, "players", List.of()));
            }
        }

        @Override public String toString() { return "CraftTeam{" + name + "}"; }
    }

    static Criteria criteria(String name) { return Stubs.proxy(Criteria.class, new CriteriaImpl(name)); }

    public static final class CriteriaImpl {
        final String name;

        CriteriaImpl(String name) { this.name = name; }

        public String getName() { return name; }
        public boolean isReadOnly() { return !name.equals("dummy") && !name.equals("trigger"); }
        public RenderType getDefaultRenderType() { return name.equals("health") ? RenderType.HEARTS : RenderType.INTEGER; }
        @Override public boolean equals(Object o) { return o instanceof CriteriaImpl c && c.name.equals(name); }
        @Override public int hashCode() { return name.hashCode(); }
    }
}
