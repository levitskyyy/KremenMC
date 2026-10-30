package kremenmc.bridge;

import java.util.Map;
import java.util.UUID;
import org.bukkit.Color;
import org.bukkit.Location;
import org.bukkit.Particle;
import org.bukkit.block.data.BlockData;

/** spawnParticle for players and worlds, sent through the "particle" action. */
final class Particles {
    private Particles() {}

    static Map<String, Object> data(Object d) {
        Map<String, Object> m = new java.util.LinkedHashMap<>();
        switch (d) {
            case null -> {}
            case Particle.DustTransition t -> {
                m.put("color", t.getColor().asRGB());
                m.put("toColor", t.getToColor().asRGB());
                m.put("scale", (double) t.getSize());
            }
            case Particle.DustOptions o -> {
                m.put("color", o.getColor().asRGB());
                m.put("scale", (double) o.getSize());
            }
            case Color c -> m.put("color", c.asARGB());
            case BlockData bd -> m.put("block", bd.getAsString());
            case org.bukkit.Material mat -> m.put("block", mat.getKey().toString());
            case Float f -> {
                m.put("roll", (double) f);
                m.put("power", (double) f);
            }
            case Integer i -> m.put("delay", i);
            default -> {
                if (d instanceof Particle.Spell s) {
                    m.put("color", s.getColor().asARGB());
                    m.put("power", (double) s.getPower());
                }
            }
        }
        return m;
    }

    static void spawn(Bridge b, UUID player, String world, Particle p, double x, double y, double z, int count,
                      double ox, double oy, double oz, double extra, Object data, boolean force) {
        if (p == null) return;
        Object[] kv = {"name", p.getKey().toString(), "x", x, "y", y, "z", z, "count", count,
                "dx", ox, "dy", oy, "dz", oz, "speed", extra, "force", force, "data", data(data),
                player == null ? "world" : "player", player == null ? world : player};
        b.action("particle", kv);
    }

    /** spawnParticle overloads shared by Player and World; target() picks the receivers. */
    interface Spawner {
        void particle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra, Object data, boolean force);

        default void spawnParticle(Particle p, Location l, int count) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count); }
        default void spawnParticle(Particle p, double x, double y, double z, int count) { particle(p, x, y, z, count, 0, 0, 0, 1, null, false); }
        default void spawnParticle(Particle p, Location l, int count, Object data) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, data); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, Object data) { particle(p, x, y, z, count, 0, 0, 0, 1, data, false); }
        default void spawnParticle(Particle p, Location l, int count, double ox, double oy, double oz) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, ox, oy, oz); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz) { particle(p, x, y, z, count, ox, oy, oz, 1, null, false); }
        default void spawnParticle(Particle p, Location l, int count, double ox, double oy, double oz, Object data) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, ox, oy, oz, data); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, Object data) { particle(p, x, y, z, count, ox, oy, oz, 1, data, false); }
        default void spawnParticle(Particle p, Location l, int count, double ox, double oy, double oz, double extra) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, ox, oy, oz, extra); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra) { particle(p, x, y, z, count, ox, oy, oz, extra, null, false); }
        default void spawnParticle(Particle p, Location l, int count, double ox, double oy, double oz, double extra, Object data) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, ox, oy, oz, extra, data); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra, Object data) { particle(p, x, y, z, count, ox, oy, oz, extra, data, false); }
        default void spawnParticle(Particle p, Location l, int count, double ox, double oy, double oz, double extra, Object data, boolean force) { spawnParticle(p, l.getX(), l.getY(), l.getZ(), count, ox, oy, oz, extra, data, force); }
        default void spawnParticle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra, Object data, boolean force) { particle(p, x, y, z, count, ox, oy, oz, extra, data, force); }
    }
}
