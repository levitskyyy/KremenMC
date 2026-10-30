package kremenmc.bridge;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;
import net.kyori.adventure.text.Component;
import org.bukkit.Location;
import org.bukkit.World;
import org.bukkit.entity.Entity;
import org.bukkit.entity.EntityType;

/** Proxies for Go-owned non-player entities, built from entity maps (see protocol_world.go). */
final class Entities {
    private Entities() {}

    private static final Map<String, EntityType> BY_KEY = new HashMap<>();

    static {
        for (EntityType t : EntityType.values()) {
            if (t != EntityType.UNKNOWN) BY_KEY.put("minecraft:" + t.name().toLowerCase(Locale.ROOT), t);
        }
    }

    static EntityType type(String key) {
        EntityType t = BY_KEY.get(key.contains(":") ? key : "minecraft:" + key);
        return t == null ? EntityType.UNKNOWN : t;
    }

    static Entity of(Bridge b, Map<String, Object> m) {
        if (m == null || !(m.get("uuid") instanceof UUID id)) return null;
        if ("minecraft:player".equals(Wire.str(m, "type"))) return b.players.get(id);
        EntityType t = type(Wire.str(m, "type"));
        Class<? extends Entity> c = t.getEntityClass();
        if (c == null || !c.isInterface()) c = Entity.class;
        return Stubs.proxy(c, new EntityImpl(b, id, t, m));
    }

    static Entity get(Bridge b, UUID id) {
        org.bukkit.entity.Player p = b.players.cached(id);
        if (p != null) return p;
        return of(b, Wire.map(b.query("entity", "uuid", id)));
    }

    static List<Entity> list(Bridge b, Object result) {
        List<Entity> out = new ArrayList<>();
        for (Object o : Wire.list(result)) {
            Entity e = of(b, Wire.map(o));
            if (e != null) out.add(e);
        }
        return out;
    }

    static List<Entity> nearby(Bridge b, UUID self, Location l, double x, double y, double z) {
        List<Entity> out = new ArrayList<>();
        if (l.getWorld() == null) return out;
        Object r = b.query("entities", "world", l.getWorld().getName(), "x", l.getX(), "y", l.getY(), "z", l.getZ(), "radius", Math.sqrt(x * x + y * y + z * z));
        for (Entity e : list(b, r)) {
            Location o = e.getLocation();
            if (!e.getUniqueId().equals(self) && Math.abs(o.getX() - l.getX()) <= x && Math.abs(o.getY() - l.getY()) <= y && Math.abs(o.getZ() - l.getZ()) <= z) out.add(e);
        }
        return out;
    }

    public static final class EntityImpl extends Effects.Living {
        final UUID id;
        final EntityType type;
        volatile Map<String, Object> snapshot;

        EntityImpl(Bridge b, UUID id, EntityType type, Map<String, Object> m) {
            super(b);
            this.id = id;
            this.type = type;
            this.snapshot = m;
        }

        @Override UUID target() { return id; }

        Map<String, Object> fresh() {
            Map<String, Object> m = Wire.map(b.query("entity", "uuid", id));
            if (m.isEmpty()) {
                Map<String, Object> dead = new HashMap<>(snapshot);
                dead.put("valid", false);
                snapshot = dead;
            } else {
                snapshot = m;
            }
            return snapshot;
        }

        @Override public void sendMessage(String msg) {}
        @Override public void sendMessage(Component c) {}
        @Override public boolean hasPermission(String perm) { return false; }

        public UUID getUniqueId() { return id; }
        public int getEntityId() { return (int) Wire.lng(snapshot, "entityId"); }
        public EntityType getType() { return type; }
        public String getName() { return type == EntityType.UNKNOWN ? Wire.str(snapshot, "type") : type.name().toLowerCase(Locale.ROOT); }
        public Component name() { return Component.translatable(type.translationKey()); }
        public boolean isValid() { return Wire.bool(fresh(), "valid"); }
        public boolean isDead() { return !isValid(); }
        public void remove() { b.action("entity_remove", "uuid", id); }
        public World getWorld() { return b.worlds.get(Wire.str(snapshot, "world")); }
        public Location getLocation() { return Players.location(b, fresh()); }

        public Location getLocation(Location into) {
            if (into == null) return null;
            Location l = getLocation();
            into.setWorld(l.getWorld());
            into.set(l.getX(), l.getY(), l.getZ());
            into.setYaw(l.getYaw());
            into.setPitch(l.getPitch());
            return into;
        }

        public double getHealth() { return Wire.dbl(fresh(), "health"); }
        public double getMaxHealth() { return Wire.dbl(snapshot, "maxHealth"); }
        public boolean isOp() { return false; }

        public List<Entity> getNearbyEntities(double x, double y, double z) { return nearby(b, id, getLocation(), x, y, z); }

        @Override public boolean equals(Object o) { return o instanceof EntityImpl e && e.id.equals(id); }
        @Override public int hashCode() { return id.hashCode(); }
        @Override public String toString() { return "CraftEntity{type=" + Wire.str(snapshot, "type") + ", uuid=" + id + "}"; }
    }
}
