package kremenmc.bridge;

import com.destroystokyo.paper.profile.PlayerProfile;
import com.destroystokyo.paper.profile.ProfileProperty;
import com.google.common.collect.LinkedHashMultimap;
import com.google.common.collect.Multimap;
import com.mojang.authlib.GameProfile;
import com.mojang.authlib.properties.Property;
import com.mojang.authlib.properties.PropertyMap;
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
import net.minecraft.server.level.ServerPlayer;
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

    public static final class PlayerImpl extends PlayerExtras {
        final UUID id;
        final String name;
        volatile Map<String, Object> snapshot = Map.of();
        volatile ServerPlayer handle;
        volatile Nms.Out out;

        PlayerImpl(Bridge b, UUID id, String name) {
            super(b);
            this.id = id;
            this.name = name;
        }

        @Override UUID target() { return id; }
        @Override String playerName() { return name; }

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
        public int getEntityId() {
            Map<String, Object> m = snapshot.containsKey("entityId") ? snapshot : fresh();
            return (int) Wire.lng(m, "entityId");
        }

        public Component displayName() {
            Component c = Text.component(Wire.str(snapshot, "displayName"));
            return c == null ? Component.text(name) : c;
        }

        public String getDisplayName() { return Text.legacy(displayName()); }
        public Component playerListName() { return displayName(); }
        public String getPlayerListName() { return getDisplayName(); }

        public PlayerProfile getPlayerProfile() {
            PlayerProfile p = profile(id, name);
            for (Property pr : getProfile().properties().values()) p.setProperty(new ProfileProperty(pr.name(), pr.value(), pr.signature()));
            return p;
        }

        public GameProfile getProfile() {
            Map<String, Object> m = snapshot.containsKey("properties") ? snapshot : fresh();
            Multimap<String, Property> props = LinkedHashMultimap.create();
            for (Object o : Wire.list(m.get("properties"))) {
                Map<String, Object> pm = Wire.map(o);
                String n = Wire.str(pm, "name");
                props.put(n, new Property(n, Wire.str(pm, "value"), pm.get("signature") instanceof String s && !s.isEmpty() ? s : null));
            }
            return new GameProfile(id, name, new PropertyMap(props));
        }

        public ServerPlayer getHandle() {
            ServerPlayer h = handle;
            if (h == null) {
                synchronized (this) {
                    h = handle;
                    if (h == null) handle = h = Nms.newHandle(b, this);
                }
            }
            return h;
        }

        public boolean isOnline() { return b.players.cached(id) != null; }
        public boolean isConnected() { return isOnline(); }
        public boolean isValid() { return isOnline(); }
        public boolean isDead() { return false; }
        public boolean isOp() { return Wire.bool(fresh(), "op"); }

        @Override
        public boolean hasPermission(String perm) {
            org.bukkit.permissions.Permission p = b.pluginManager.getPermission(perm);
            String def = p == null ? "" : switch (p.getDefault()) {
                case TRUE -> "true";
                case FALSE -> "false";
                case OP -> "op";
                case NOT_OP -> "notop";
            };
            return Wire.bool(b.call(Wire.OP_HAS_PERMISSION, id, perm, def), 0);
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

        public boolean teleport(Location l) { return teleport(l, PlayerTeleportEvent.TeleportCause.PLUGIN); }

        public boolean teleport(Location l, PlayerTeleportEvent.TeleportCause cause) {
            PlayerTeleportEvent e = new PlayerTeleportEvent(self(), getLocation(), l.clone(), cause);
            b.pluginManager.callEvent(e);
            if (e.isCancelled() || e.getTo() == null) return false;
            return Wire.bool(b.call(Wire.OP_TELEPORT, id, locationMap(e.getTo())), 0);
        }
        public boolean teleport(Entity e) { return teleport(e.getLocation()); }
        public boolean teleport(Entity e, PlayerTeleportEvent.TeleportCause cause) { return teleport(e.getLocation()); }
        public CompletableFuture<Boolean> teleportAsync(Location l) { return CompletableFuture.completedFuture(teleport(l)); }
        public CompletableFuture<Boolean> teleportAsync(Location l, PlayerTeleportEvent.TeleportCause c) { return teleportAsync(l); }

        public void kick() { kick(Component.translatable("multiplayer.disconnect.kicked")); }
        public void kick(Component reason) { b.conn.notify(Wire.OP_KICK, id, Text.json(reason == null ? Component.empty() : reason)); }
        public void kickPlayer(String reason) { b.conn.notify(Wire.OP_KICK, id, Text.jsonLegacy(reason == null ? "" : reason)); }

        public boolean performCommand(String cmd) { return Bukkit.dispatchCommand(self(), cmd); }

        Map<String, Object> state() { return Wire.map(b.query("player_state", "player", id)); }
        private void set(String field, Object... kv) {
            Object[] a = new Object[kv.length + 4];
            a[0] = "player"; a[1] = id; a[2] = "field"; a[3] = field;
            System.arraycopy(kv, 0, a, 4, kv.length);
            b.action("player_set", a);
        }

        public double getHealth() { return Wire.dbl(state(), "health"); }
        public void setHealth(double v) {
            if (v < 0 || v > getMaxHealth()) throw new IllegalArgumentException("Health value (" + v + ") must be between 0 and " + getMaxHealth());
            set("health", "value", v);
        }
        public double getMaxHealth() { return Wire.dbl(state(), "maxHealth"); }
        public int getFoodLevel() { return (int) Wire.lng(state(), "food"); }
        public void setFoodLevel(int v) { set("food", "value", v); }
        public float getSaturation() { return (float) Wire.dbl(state(), "saturation"); }
        public void setSaturation(float v) { set("saturation", "value", (double) v); }
        public float getExp() { return (float) Wire.dbl(state(), "exp"); }
        public int getLevel() { return (int) Wire.lng(state(), "level"); }
        public void setExp(float v) { set("exp", "level", getLevel(), "progress", (double) v); }
        public void setLevel(int v) { set("exp", "level", v, "progress", (double) getExp()); }
        public void giveExpLevels(int n) { setLevel(Math.max(0, getLevel() + n)); }
        public boolean isFlying() { return Wire.bool(state(), "flying"); }
        public void setFlying(boolean v) { set("flying", "value", v); }
        public boolean getAllowFlight() { return Wire.bool(state(), "allowFlight"); }
        public void setAllowFlight(boolean v) { set("allowFlight", "value", v); }
        public void setGameMode(GameMode g) { set("gameMode", "value", g.name()); }
        public boolean isHealthScaled() { return false; }

        public void sendActionBar(Component c) { b.action("action_bar", "player", id, "text", Text.json(c)); }
        public void sendActionBar(String s) { b.action("action_bar", "player", id, "text", Text.jsonLegacy(s)); }
        public void showTitle(net.kyori.adventure.title.Title t) {
            net.kyori.adventure.title.Title.Times tm = t.times();
            long in = tm == null ? 10 : tm.fadeIn().toMillis() / 50, stay = tm == null ? 70 : tm.stay().toMillis() / 50, out = tm == null ? 20 : tm.fadeOut().toMillis() / 50;
            b.action("title", "player", id, "title", Text.json(t.title()), "subtitle", Text.json(t.subtitle()), "fadeIn", in, "stay", stay, "fadeOut", out);
        }
        public void sendTitle(String title, String subtitle) { sendTitle(title, subtitle, 10, 70, 20); }
        public void sendTitle(String title, String subtitle, int in, int stay, int out) {
            b.action("title", "player", id, "title", Text.jsonLegacy(title == null ? "" : title), "subtitle", Text.jsonLegacy(subtitle == null ? "" : subtitle), "fadeIn", in, "stay", stay, "fadeOut", out);
        }
        public void clearTitle() { b.action("title", "player", id, "clear", true); }
        public void resetTitle() { clearTitle(); }
        public void playSound(net.kyori.adventure.sound.Sound s) {
            b.action("sound", "player", id, "name", s.name().asString(), "category", s.source().name().toLowerCase(Locale.ROOT), "volume", (double) s.volume(), "pitch", (double) s.pitch(), "seed", s.seed().orElse(0L));
        }
        public void playSound(net.kyori.adventure.sound.Sound s, double x, double y, double z) {
            b.action("sound", "player", id, "name", s.name().asString(), "category", s.source().name().toLowerCase(Locale.ROOT), "volume", (double) s.volume(), "pitch", (double) s.pitch(), "seed", s.seed().orElse(0L), "x", x, "y", y, "z", z);
        }
        public void playSound(Location l, String sound, float volume, float pitch) {
            b.action("sound", "player", id, "name", sound.contains(":") ? sound : "minecraft:" + sound, "category", "master", "volume", (double) volume, "pitch", (double) pitch, "seed", 0L, "x", l.getX(), "y", l.getY(), "z", l.getZ());
        }
        public void playSound(Location l, org.bukkit.Sound sound, float volume, float pitch) {
            playSound(l, sound.key().asString(), volume, pitch);
        }
        public String getLocale() { return "en_us"; }
        public Locale locale() { return Locale.US; }
        public int getPing() { return (int) Wire.lng(fresh(), "ping"); }

        public EntityScheduler getScheduler() { return b.scheduler.entityScheduler(self()); }

        public void sendPlayerListHeaderAndFooter(Component header, Component footer) {
            b.conn.notify(Wire.OP_SEND_PACKET, id, "tab_list", Map.of("header", Text.json(header), "footer", Text.json(footer)));
        }
        public void sendPlayerListHeader(Component header) { sendPlayerListHeaderAndFooter(header, Component.empty()); }
        public void sendPlayerListFooter(Component footer) { sendPlayerListHeaderAndFooter(Component.empty(), footer); }
        public void setPlayerListHeaderFooter(String header, String footer) {
            b.conn.notify(Wire.OP_SEND_PACKET, id, "tab_list", Map.of("header", Text.jsonLegacy(header == null ? "" : header), "footer", Text.jsonLegacy(footer == null ? "" : footer)));
        }
        public org.bukkit.inventory.PlayerInventory getInventory() { return Inventories.playerInventory(b, id); }
        public org.bukkit.inventory.EntityEquipment getEquipment() { return Inventories.equipment(b, id); }
        public org.bukkit.inventory.Inventory getEnderChest() { return Inventories.enderChest(b, id); }
        public org.bukkit.inventory.InventoryView getOpenInventory() { return Inventories.openView(b, self()); }
        public org.bukkit.inventory.InventoryView openInventory(org.bukkit.inventory.Inventory inv) { return Inventories.openInventory(b, self(), inv); }
        public void openInventory(org.bukkit.inventory.InventoryView v) { if (v != null) Inventories.openInventory(b, self(), v.getTopInventory()); }
        public void closeInventory() { Inventories.closeInventory(b, self(), org.bukkit.event.inventory.InventoryCloseEvent.Reason.PLUGIN); }
        public void closeInventory(org.bukkit.event.inventory.InventoryCloseEvent.Reason r) { Inventories.closeInventory(b, self(), r); }
        public org.bukkit.inventory.ItemStack getItemOnCursor() { return getOpenInventory().getCursor(); }
        public void setItemOnCursor(org.bukkit.inventory.ItemStack s) { getOpenInventory().setCursor(s); }
        public org.bukkit.inventory.ItemStack getItemInHand() { return getInventory().getItemInMainHand(); }
        public void setItemInHand(org.bukkit.inventory.ItemStack s) { getInventory().setItemInMainHand(s); }

        public Player.Spigot spigot() {
            PlayerImpl me = this;
            return new Player.Spigot() {
                @Override public void sendMessage(BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(BaseComponent... cs) { me.sendMessage(cs); }
                @Override public void sendMessage(ChatMessageType t, BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(ChatMessageType t, BaseComponent... cs) { me.sendMessage(cs); }
                @Override public void sendMessage(ChatMessageType t, UUID s, BaseComponent c) { me.sendMessage(c); }
                @Override public void sendMessage(ChatMessageType t, UUID s, BaseComponent... cs) { me.sendMessage(cs); }
                @Override public int getPing() { return me.getPing(); }
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
