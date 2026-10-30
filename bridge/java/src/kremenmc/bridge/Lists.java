package kremenmc.bridge;

import com.destroystokyo.paper.profile.PlayerProfile;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.time.Instant;
import java.util.Date;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import org.bukkit.BanEntry;
import org.bukkit.BanList;
import org.bukkit.OfflinePlayer;

/** ops.json, whitelist.json and the ban lists, owned by Go. */
final class Lists {
    private Lists() {}

    static List<Object> entries(Bridge b, String list) { return Wire.list(b.query("list", "list", list)); }

    static boolean contains(Bridge b, String list, UUID id) {
        for (Object o : entries(b, list)) if (id.equals(Wire.map(o).get("target"))) return true;
        return false;
    }

    static void setOp(Bridge b, UUID id, String name, boolean v) { b.action("op_set", "uuid", id, "name", name == null ? "" : name, "value", v); }
    static boolean whitelisted(Bridge b, UUID id) { return contains(b, "whitelist", id); }
    static void setWhitelisted(Bridge b, UUID id, String name, boolean v) { b.action("whitelist_set", "uuid", id, "name", name == null ? "" : name, "value", v); }
    static boolean whitelistOn(Bridge b) { return Wire.bool(Wire.map(b.query("list_state")), "whitelist"); }

    static Map<String, Object> entry(Bridge b, String list, String target) {
        for (Object o : entries(b, list)) {
            Map<String, Object> m = Wire.map(o);
            if (Wire.str(m, "target").equalsIgnoreCase(target)) return m;
        }
        return null;
    }

    static boolean banned(Bridge b, String list, String target) { return entry(b, list, target) != null; }

    static BanEntry<?> ban(Bridge b, String list, String target, String name, String reason, Instant expires, String source) {
        b.action("ban", "list", list, "target", target, "name", name == null ? "" : name, "reason", reason == null ? "" : reason,
                "source", source == null ? "" : source, "expires", expires == null ? 0L : expires.toEpochMilli());
        Map<String, Object> m = Bridge.fields("target", target, "name", name == null ? "" : name, "reason", reason == null ? "Banned by an operator." : reason,
                "source", source == null || source.isEmpty() ? "Server" : source, "created", System.currentTimeMillis(), "expires", expires == null ? 0L : expires.toEpochMilli());
        return entryProxy(b, list, m);
    }

    static BanEntry<?> entryProxy(Bridge b, String list, Map<String, Object> m) {
        return Stubs.proxy(BanEntry.class, new EntryImpl(b, list, m));
    }

    static Set<OfflinePlayer> players(Bridge b, String list) {
        Set<OfflinePlayer> out = new HashSet<>();
        for (Object o : entries(b, list)) {
            Map<String, Object> m = Wire.map(o);
            UUID id = m.get("target") instanceof UUID u ? u : parse(Wire.str(m, "target"));
            if (id != null) out.add(b.server.getOfflinePlayer(id));
        }
        return out;
    }

    static UUID parse(String s) {
        try {
            return UUID.fromString(s);
        } catch (IllegalArgumentException e) {
            return null;
        }
    }

    public static final class EntryImpl {
        final Bridge b;
        final String list;
        final String target;
        String name, reason, source;
        long created, expires;

        EntryImpl(Bridge b, String list, Map<String, Object> m) {
            this.b = b;
            this.list = list;
            this.target = Wire.str(m, "target");
            this.name = Wire.str(m, "name");
            this.reason = Wire.str(m, "reason");
            this.source = Wire.str(m, "source");
            this.created = Wire.lng(m, "created");
            this.expires = Wire.lng(m, "expires");
        }

        public String getTarget() { return list.equals("ip") ? target : (name.isEmpty() ? target : name); }

        public Object getBanTarget() {
            if (list.equals("ip")) {
                try {
                    return InetAddress.ofLiteral(target);
                } catch (IllegalArgumentException e) {
                    return null;
                }
            }
            return Players.profile(parse(target), name.isEmpty() ? null : name);
        }

        public Date getCreated() { return new Date(created); }
        public void setCreated(Date d) { created = d == null ? System.currentTimeMillis() : d.getTime(); }
        public String getSource() { return source; }
        public void setSource(String s) { source = s; }
        public Date getExpiration() { return expires == 0 ? null : new Date(expires); }
        public void setExpiration(Date d) { expires = d == null ? 0 : d.getTime(); }
        public String getReason() { return reason; }
        public void setReason(String r) { reason = r; }

        public void save() {
            b.action("ban", "list", list, "target", target, "name", name, "reason", reason, "source", source, "expires", expires);
        }

        public void remove() { b.action("pardon", "list", list, "target", target); }

        @Override public String toString() { return "BanEntry{" + target + "}"; }
    }

    static BanList<?> banList(Bridge b, boolean ip) {
        return Stubs.proxy(BanList.class, new ListImpl(b, ip ? "ip" : "profile"));
    }

    /** BanList for profiles (targets are UUID strings) or IP addresses. */
    public static final class ListImpl {
        final Bridge b;
        final String list;

        ListImpl(Bridge b, String list) {
            this.b = b;
            this.list = list;
        }

        String key(Object t) {
            return switch (t) {
                case null -> "";
                case PlayerProfile p -> p.getId() != null ? p.getId().toString() : uuidOf(p.getName());
                case OfflinePlayer p -> p.getUniqueId().toString();
                case UUID u -> u.toString();
                case InetAddress a -> a.getHostAddress();
                case InetSocketAddress a -> a.getAddress() == null ? a.getHostString() : a.getAddress().getHostAddress();
                case String s -> list.equals("profile") && parse(s) == null ? uuidOf(s) : s;
                default -> String.valueOf(t);
            };
        }

        String uuidOf(String name) {
            if (name == null) return "";
            org.bukkit.entity.Player p = b.players.byName(name, true);
            if (p != null) return p.getUniqueId().toString();
            for (Object o : entries(b, list)) {
                Map<String, Object> m = Wire.map(o);
                if (Wire.str(m, "name").equalsIgnoreCase(name)) return Wire.str(m, "target");
            }
            return name;
        }

        String nameOf(Object t) {
            return switch (t) {
                case PlayerProfile p -> p.getName();
                case OfflinePlayer p -> p.getName();
                case String s -> s;
                default -> "";
            };
        }

        public BanEntry<?> getBanEntry(String t) { return getBanEntry((Object) t); }

        public BanEntry<?> getBanEntry(Object t) {
            Map<String, Object> m = entry(b, list, key(t));
            return m == null ? null : entryProxy(b, list, m);
        }

        public BanEntry<?> addBan(String t, String reason, Date expires, String source) { return addBan((Object) t, reason, expires, source); }
        public BanEntry<?> addBan(Object t, String reason, Date expires, String source) { return addBan(t, reason, expires == null ? null : expires.toInstant(), source); }
        public BanEntry<?> addBan(Object t, String reason, Duration d, String source) { return addBan(t, reason, d == null ? null : Instant.now().plus(d), source); }
        public BanEntry<?> addBan(Object t, String reason, Instant expires, String source) { return ban(b, list, key(t), nameOf(t), reason, expires, source); }

        public Set<BanEntry> getBanEntries() {
            Set<BanEntry> out = new HashSet<>();
            for (Object o : entries(b, list)) out.add(entryProxy(b, list, Wire.map(o)));
            return out;
        }

        public Set<BanEntry> getEntries() { return getBanEntries(); }
        public boolean isBanned(String t) { return isBanned((Object) t); }
        public boolean isBanned(Object t) { return banned(b, list, key(t)); }
        public void pardon(String t) { pardon((Object) t); }
        public void pardon(Object t) { b.action("pardon", "list", list, "target", key(t)); }
    }
}
