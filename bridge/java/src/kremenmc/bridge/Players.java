package kremenmc.bridge;

import com.destroystokyo.paper.profile.PlayerProfile;
import com.destroystokyo.paper.profile.ProfileProperty;
import io.papermc.paper.threadedregions.scheduler.EntityScheduler;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import net.kyori.adventure.chat.ChatType;
import net.kyori.adventure.text.Component;
import net.md_5.bungee.api.ChatMessageType;
import net.md_5.bungee.api.chat.BaseComponent;
import org.bukkit.Bukkit;
import org.bukkit.GameMode;
import org.bukkit.Location;
import org.bukkit.Server;
import org.bukkit.World;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Entity;
import org.bukkit.entity.EntityType;
import org.bukkit.entity.Player;
import org.bukkit.event.player.PlayerTeleportEvent;
import org.bukkit.permissions.Permission;

/** Online player proxies. Go owns player state; proxies fetch it on demand. */
final class Players {
    private final Bridge b;
    private final Map<UUID, Player> online = new ConcurrentHashMap<>();

    Players(Bridge b) { this.b = b; }

    Player cached(UUID id) { return online.get(id); }

    Player get(UUID id) {
        Player p = online.get(id);
        if (p != null) return p;
        Map<String, Object> m = Wire.map(b.call(Wire.OP_PLAYER, id).get(0));
        return m.isEmpty() ? null : track(m);
    }

    Player track(UUID id, String name) {
        return online.computeIfAbsent(id, k -> Stubs.proxy(Player.class, new PlayerImpl(b, k, name)));
    }

    Player track(Map<String, Object> m) {
        UUID id = (UUID) m.get("uuid");
        Player p = track(id, Wire.str(m, "name"));
        ((PlayerImpl) Stubs.implOf(p)).snapshot = m;
        return p;
    }

    void remove(UUID id) { online.remove(id); }

    List<Player> online() {
        List<Player> out = new ArrayList<>();
        Set<UUID> seen = new HashSet<>();
        for (Object o : Wire.list(b.call(Wire.OP_PLAYERS).get(0))) {
            Map<String, Object> m = Wire.map(o);
            if (m.get("uuid") instanceof UUID) {
                Player p = track(m);
                seen.add(p.getUniqueId());
                out.add(p);
            }
        }
        online.keySet().retainAll(seen);
        return out;
    }

    Player byName(String name, boolean exact) {
        for (Player p : online.values()) if (p.getName().equalsIgnoreCase(name)) return p;
        Map<String, Object> m = Wire.map(b.call(Wire.OP_PLAYER, name).get(0));
        if (!m.isEmpty()) return track(m);
        if (exact) return null;
        String lower = name.toLowerCase(Locale.ROOT);
        Player best = null;
        int delta = Integer.MAX_VALUE;
        for (Player p : online()) {
            String n = p.getName().toLowerCase(Locale.ROOT);
            if (n.startsWith(lower) && n.length() - lower.length() < delta) {
                best = p;
                delta = n.length() - lower.length();
            }
        }
        return best;
    }

    static Location location(Bridge b, Map<String, Object> m) {
        World w = b.worlds.get(Wire.str(m, "world"));
        return new Location(w, Wire.dbl(m, "x"), Wire.dbl(m, "y"), Wire.dbl(m, "z"), (float) Wire.dbl(m, "yaw"), (float) Wire.dbl(m, "pitch"));
    }

    static Map<String, Object> locationMap(Location l) {
        return Map.of("world", l.getWorld() == null ? "" : l.getWorld().getName(),
                "x", l.getX(), "y", l.getY(), "z", l.getZ(), "yaw", (double) l.getYaw(), "pitch", (double) l.getPitch());
    }

    /** Methods shared by every CommandSender proxy. */
    abstract static class SenderImpl {
        final Bridge b;
        SenderImpl(Bridge b) { this.b = b; }

        abstract UUID target();

        public Server getServer() { return b.server; }
        public void sendMessage(String msg) { b.sendMessage(target(), Text.jsonLegacy(msg)); }
        public void sendMessage(String... msgs) { for (String m : msgs) sendMessage(m); }
        public void sendMessage(UUID sender, String msg) { sendMessage(msg); }
        public void sendMessage(UUID sender, String... msgs) { sendMessage(msgs); }
        public void sendMessage(Component c) { b.sendMessage(target(), Text.json(c)); }
        public void sendMessage(Component c, ChatType.Bound bound) { sendMessage(c); }
        public void sendMessage(BaseComponent c) { b.sendMessage(target(), Text.json(c)); }
        public void sendMessage(BaseComponent... cs) { b.sendMessage(target(), Text.json(cs)); }
        public void sendPlainMessage(String msg) { sendMessage(Component.text(msg)); }
        public boolean isPermissionSet(String perm) { return true; }
        public boolean isPermissionSet(Permission perm) { return true; }
        public boolean hasPermission(Permission perm) { return hasPermission(perm.getName()); }
        public abstract boolean hasPermission(String perm);
    }

    public static final class ConsoleImpl extends SenderImpl {
        ConsoleImpl(Bridge b) { super(b); }
        @Override UUID target() { return null; }
        public String getName() { return "CONSOLE"; }
        public Component name() { return Component.text("CONSOLE"); }
        @Override public boolean hasPermission(String perm) { return true; }
        public boolean isOp() { return true; }
        public void setOp(boolean op) {}
        public boolean isConversing() { return false; }
        public CommandSender.Spigot spigot() { return new CommandSender.Spigot(); }
        @Override public String toString() { return "ConsoleCommandSender"; }
    }

    public static final class PlayerImpl extends SenderImpl {
        final UUID id;
        final String name;
        volatile Map<String, Object> snapshot = Map.of();

        PlayerImpl(Bridge b, UUID id, String name) {
            super(b);
            this.id = id;
            this.name = name;
        }

        @Override UUID target() { return id; }

        private Player self() { return b.players.cached(id); }

        Map<String, Object> fresh() {
            Map<String, Object> m = Wire.map(b.call(Wire.OP_PLAYER, id).get(0));
            if (!m.isEmpty()) snapshot = m;
            return snapshot;
        }

        public String getName() { return name; }
        public UUID getUniqueId() { return id; }
        public Component name() { return Component.text(name); }
        public EntityType getType() { return EntityType.PLAYER; }
        public int getEntityId() { return id.hashCode() & 0x7fffffff; }

        public Component displayName() {
            Component c = Text.component(Wire.str(snapshot, "displayName"));
            return c == null ? Component.text(name) : c;
        }

        public String getDisplayName() { return Text.legacy(displayName()); }
        public Component playerListName() { return displayName(); }
        public String getPlayerListName() { return getDisplayName(); }

        public PlayerProfile getPlayerProfile() { return profile(id, name); }

        public boolean isOnline() { return b.players.cached(id) != null; }
        public boolean isConnected() { return isOnline(); }
        public boolean isValid() { return isOnline(); }
        public boolean isDead() { return false; }
        public boolean isOp() { return Wire.bool(fresh(), "op"); }

        @Override
        public boolean hasPermission(String perm) {
            return Wire.bool(b.call(Wire.OP_HAS_PERMISSION, id, perm), 0);
        }

        public GameMode getGameMode() {
            try {
                return GameMode.valueOf(Wire.str(fresh(), "gameMode"));
            } catch (IllegalArgumentException e) {
                return GameMode.SURVIVAL;
            }
        }

        public InetSocketAddress getAddress() {
            String a = Wire.str(fresh(), "address");
            int i = a.lastIndexOf(':');
            if (i < 0) return null;
            String host = a.substring(0, i).replace("[", "").replace("]", "");
            return InetSocketAddress.createUnresolved(host, Integer.parseInt(a.substring(i + 1)));
        }

        public Location getLocation() { return location(b, fresh()); }

        public Location getLocation(Location into) {
            if (into == null) return null;
            Location l = getLocation();
            into.setWorld(l.getWorld());
            into.set(l.getX(), l.getY(), l.getZ());
            into.setYaw(l.getYaw());
            into.setPitch(l.getPitch());
            return into;
        }

        public World getWorld() { return b.worlds.get(Wire.str(fresh(), "world")); }

        public boolean teleport(Location l) {
            return Wire.bool(b.call(Wire.OP_TELEPORT, id, locationMap(l)), 0);
        }

        public boolean teleport(Location l, PlayerTeleportEvent.TeleportCause cause) { return teleport(l); }
        public boolean teleport(Entity e) { return teleport(e.getLocation()); }
        public boolean teleport(Entity e, PlayerTeleportEvent.TeleportCause cause) { return teleport(e.getLocation()); }
        public CompletableFuture<Boolean> teleportAsync(Location l) { return CompletableFuture.completedFuture(teleport(l)); }
        public CompletableFuture<Boolean> teleportAsync(Location l, PlayerTeleportEvent.TeleportCause c) { return teleportAsync(l); }

        public void kick() { kick(Component.translatable("multiplayer.disconnect.kicked")); }
        public void kick(Component reason) { b.conn.notify(Wire.OP_KICK, id, Text.json(reason == null ? Component.empty() : reason)); }
        public void kickPlayer(String reason) { b.conn.notify(Wire.OP_KICK, id, Text.jsonLegacy(reason == null ? "" : reason)); }

        public boolean performCommand(String cmd) { return Bukkit.dispatchCommand(self(), cmd); }

        public double getHealth() { return 20; }
        public int getFoodLevel() { return 20; }
        public String getLocale() { return "en_us"; }
        public Locale locale() { return Locale.US; }
        public int getPing() { return 0; }

        public EntityScheduler getScheduler() { return b.scheduler.entityScheduler(self()); }

        public Player.Spigot spigot() {
            PlayerImpl me = this;
            return new Player.Spigot() {
                @Override public void sendMessage(BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(BaseComponent... cs) { me.sendMessage(cs); }
                @Override public void sendMessage(ChatMessageType t, BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(ChatMessageType t, BaseComponent... cs) { me.sendMessage(cs); }
                @Override public void sendMessage(ChatMessageType t, UUID s, BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(ChatMessageType t, UUID s, BaseComponent... cs) { me.sendMessage(cs); }
                @Override public int getPing() { return 0; }
            };
        }

        @Override public boolean equals(Object o) { return o instanceof PlayerImpl p && p.id.equals(id); }
        @Override public int hashCode() { return id.hashCode(); }
        @Override public String toString() { return "CraftPlayer{name=" + name + "}"; }
    }

    static PlayerProfile profile(UUID id, String name) {
        return Stubs.proxy(PlayerProfile.class, new ProfileImpl(id, name));
    }

    public static final class ProfileImpl {
        UUID id;
        String name;
        final Set<ProfileProperty> props = ConcurrentHashMap.newKeySet();

        ProfileImpl(UUID id, String name) {
            this.id = id;
            this.name = name;
        }

        public UUID getId() { return id; }
        public UUID setId(UUID v) { UUID o = id; id = v; return o; }
        public String getName() { return name; }
        public String setName(String v) { String o = name; name = v; return o; }
        public Set<ProfileProperty> getProperties() { return props; }
        public boolean hasProperty(String n) { return props.stream().anyMatch(p -> p.getName().equals(n)); }
        public void setProperty(ProfileProperty p) { removeProperty(p.getName()); props.add(p); }
        public boolean removeProperty(String n) { return props.removeIf(p -> p.getName().equals(n)); }
        public void clearProperties() { props.clear(); }
        public boolean isComplete() { return id != null && name != null; }
        public boolean complete(boolean textures) { return isComplete(); }
        public boolean completeFromCache() { return isComplete(); }
        public PlayerProfile clone() { ProfileImpl c = new ProfileImpl(id, name); c.props.addAll(props); return Stubs.proxy(PlayerProfile.class, c); }
        @Override public boolean equals(Object o) { return o instanceof ProfileImpl p && java.util.Objects.equals(p.id, id) && java.util.Objects.equals(p.name, name); }
        @Override public int hashCode() { return java.util.Objects.hash(id, name); }
        @Override public String toString() { return "PlayerProfile{id=" + id + ", name=" + name + "}"; }
    }
}
