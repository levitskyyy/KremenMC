package kremenmc.bridge;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
import java.util.UUID;
import net.kyori.adventure.text.Component;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.NamespacedKey;
import org.bukkit.Registry;
import org.bukkit.damage.DamageSource;
import org.bukkit.damage.DamageType;
import org.bukkit.entity.Entity;
import org.bukkit.entity.LivingEntity;
import org.bukkit.entity.Player;
import org.bukkit.event.Event;
import org.bukkit.event.entity.CreatureSpawnEvent;
import org.bukkit.event.entity.EntityDamageByEntityEvent;
import org.bukkit.event.entity.EntityDamageEvent;
import org.bukkit.event.entity.EntitySpawnEvent;
import org.bukkit.event.entity.PlayerDeathEvent;
import org.bukkit.event.player.PlayerRespawnEvent;
import org.bukkit.event.player.PlayerTeleportEvent;
import org.bukkit.inventory.ItemStack;

/** Damage, death, respawn, teleport and spawn events fired from Go. */
final class WorldEvents {
    private WorldEvents() {}

    static void register(Map<String, List<Class<? extends Event>>> wire) {
        wire.put("EntityDamage", List.of(EntityDamageEvent.class, EntityDamageByEntityEvent.class));
        wire.put("PlayerDeath", List.of(PlayerDeathEvent.class, org.bukkit.event.entity.EntityDeathEvent.class));
        wire.put("PlayerRespawn", List.of(PlayerRespawnEvent.class));
        wire.put("PlayerTeleport", List.of(PlayerTeleportEvent.class));
        wire.put("EntitySpawn", List.of(CreatureSpawnEvent.class, EntitySpawnEvent.class));
    }

    /** Returns null for an event name this class does not handle. */
    static Boolean dispatch(Bridge b, String name, Map<String, Object> f, Map<String, Object> out) {
        return switch (name) {
            case "EntityDamage" -> damage(b, f, out);
            case "PlayerDeath" -> death(b, f, out);
            case "PlayerRespawn" -> respawn(b, f, out);
            case "PlayerTeleport" -> teleport(b, f, out);
            case "EntitySpawn" -> spawn(b, f);
            default -> null;
        };
    }

    private static Player player(Bridge b, Map<String, Object> f) {
        UUID id = (UUID) f.get("player");
        Player p = b.players.cached(id);
        return p != null ? p : b.players.get(id);
    }

    static EntityDamageEvent.DamageCause cause(String type, boolean byEntity) {
        String t = type.startsWith("minecraft:") ? type.substring(10) : type;
        return switch (t) {
            case "player_attack", "mob_attack", "mob_attack_no_aggro", "sting" -> EntityDamageEvent.DamageCause.ENTITY_ATTACK;
            case "player_explosion", "explosion" -> byEntity ? EntityDamageEvent.DamageCause.ENTITY_EXPLOSION : EntityDamageEvent.DamageCause.BLOCK_EXPLOSION;
            case "arrow", "trident", "mob_projectile", "fireball", "unattributed_fireball", "wither_skull", "thrown", "wind_charge", "spit" -> EntityDamageEvent.DamageCause.PROJECTILE;
            case "fall", "stalagmite" -> EntityDamageEvent.DamageCause.FALL;
            case "fly_into_wall" -> EntityDamageEvent.DamageCause.FLY_INTO_WALL;
            case "in_fire", "campfire" -> EntityDamageEvent.DamageCause.FIRE;
            case "on_fire" -> EntityDamageEvent.DamageCause.FIRE_TICK;
            case "lava" -> EntityDamageEvent.DamageCause.LAVA;
            case "hot_floor" -> EntityDamageEvent.DamageCause.HOT_FLOOR;
            case "drown" -> EntityDamageEvent.DamageCause.DROWNING;
            case "in_wall" -> EntityDamageEvent.DamageCause.SUFFOCATION;
            case "cramming" -> EntityDamageEvent.DamageCause.CRAMMING;
            case "starve" -> EntityDamageEvent.DamageCause.STARVATION;
            case "cactus", "sweet_berry_bush" -> EntityDamageEvent.DamageCause.CONTACT;
            case "out_of_world" -> EntityDamageEvent.DamageCause.VOID;
            case "generic_kill" -> EntityDamageEvent.DamageCause.KILL;
            case "magic", "indirect_magic" -> EntityDamageEvent.DamageCause.MAGIC;
            case "wither" -> EntityDamageEvent.DamageCause.WITHER;
            case "lightning_bolt" -> EntityDamageEvent.DamageCause.LIGHTNING;
            case "falling_block", "falling_anvil", "falling_stalactite" -> EntityDamageEvent.DamageCause.FALLING_BLOCK;
            case "dragon_breath" -> EntityDamageEvent.DamageCause.DRAGON_BREATH;
            case "freeze" -> EntityDamageEvent.DamageCause.FREEZE;
            case "sonic_boom" -> EntityDamageEvent.DamageCause.SONIC_BOOM;
            case "thorns" -> EntityDamageEvent.DamageCause.THORNS;
            case "dry_out" -> EntityDamageEvent.DamageCause.DRYOUT;
            case "world_border" -> EntityDamageEvent.DamageCause.WORLD_BORDER;
            default -> EntityDamageEvent.DamageCause.CUSTOM;
        };
    }

    static DamageSource source(String type, Entity causing, Entity direct, Location at) {
        return Stubs.proxy(DamageSource.class, new SourceImpl(type, causing, direct, at));
    }

    public static final class SourceImpl {
        final String type;
        final Entity causing, direct;
        final Location at;

        SourceImpl(String type, Entity causing, Entity direct, Location at) {
            this.type = type.isEmpty() ? "minecraft:generic" : type;
            this.causing = causing;
            this.direct = direct;
            this.at = at;
        }

        public DamageType getDamageType() {
            NamespacedKey k = NamespacedKey.fromString(type);
            return k == null ? null : Registry.DAMAGE_TYPE.get(k);
        }

        public Entity getCausingEntity() { return causing; }
        public Entity getDirectEntity() { return direct; }
        public Location getDamageLocation() { return at; }
        public Location getSourceLocation() { return at != null ? at : direct != null ? direct.getLocation() : null; }
        public boolean isIndirect() { return causing != direct; }
        public float getFoodExhaustion() { return 0.1f; }
        public boolean scalesWithDifficulty() { return causing instanceof LivingEntity && !(causing instanceof Player); }
    }

    private static boolean damage(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Entity victim = Entities.of(b, Wire.map(f.get("entity")));
        if (victim == null) return false;
        Map<String, Object> dm = Wire.map(f.get("damager"));
        Entity damager = dm.isEmpty() ? null : Entities.of(b, dm);
        Map<String, Object> dd = Wire.map(f.get("direct"));
        Entity direct = dd.isEmpty() ? damager : Entities.of(b, dd);
        String type = Wire.str(f, "cause");
        double dmg = Wire.dbl(f, "damage");
        DamageSource src = source(type, damager, direct, null);
        EntityDamageEvent.DamageCause c = cause(type, damager != null);
        EntityDamageEvent e = direct != null ? new EntityDamageByEntityEvent(direct, victim, c, src, dmg) : new EntityDamageEvent(victim, c, src, dmg);
        b.pluginManager.callEvent(e);
        if (e.getDamage() != dmg) out.put("damage", e.getDamage());
        return e.isCancelled();
    }

    private static boolean death(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Player p = player(b, f);
        List<ItemStack> drops = new ArrayList<>();
        for (Object o : Wire.list(f.get("drops"))) {
            Map<String, Object> m = Wire.map(o);
            Material mat = Material.matchMaterial(Wire.str(m, "type"));
            if (mat == null) continue;
            try {
                drops.add(new ItemStack(mat, (int) Wire.lng(m, "count")));
            } catch (RuntimeException | LinkageError ex) {
                break;
            }
        }
        int before = drops.size();
        Component msg = Text.component(Wire.str(f, "deathMessage"));
        boolean keep = Wire.bool(f, "keepInventory");
        int level = (int) Wire.lng(f, "level");
        PlayerDeathEvent e = new PlayerDeathEvent(p, source("minecraft:generic", null, null, p.getLocation()), drops, Math.min(level * 7, 100), msg, false);
        e.setKeepInventory(keep);
        b.pluginManager.callEvent(e);
        if (!Objects.equals(msg, e.deathMessage())) {
            if (e.deathMessage() == null) out.put("hideMessage", true);
            else out.put("deathMessage", Text.json(e.deathMessage()));
        }
        if (e.getKeepInventory() != keep) out.put("keepInventory", e.getKeepInventory());
        if (before > 0 && e.getDrops().isEmpty()) out.put("clearDrops", true);
        return false;
    }

    private static boolean respawn(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Location at = Players.location(b, Wire.map(f.get("location")));
        Location orig = at.clone();
        PlayerRespawnEvent e = new PlayerRespawnEvent(player(b, f), at, false, false, false, PlayerRespawnEvent.RespawnReason.DEATH);
        b.pluginManager.callEvent(e);
        if (!orig.equals(e.getRespawnLocation())) out.put("location", Players.locationMap(e.getRespawnLocation()));
        return false;
    }

    private static boolean teleport(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Location from = Players.location(b, Wire.map(f.get("from")));
        Location to = Players.location(b, Wire.map(f.get("to")));
        Location orig = to.clone();
        PlayerTeleportEvent.TeleportCause cause;
        try {
            cause = PlayerTeleportEvent.TeleportCause.valueOf(Wire.str(f, "cause").toUpperCase(Locale.ROOT));
        } catch (IllegalArgumentException ex) {
            cause = PlayerTeleportEvent.TeleportCause.UNKNOWN;
        }
        PlayerTeleportEvent e = new PlayerTeleportEvent(player(b, f), from, to, cause);
        b.pluginManager.callEvent(e);
        if (e.getTo() != null && !orig.equals(e.getTo())) out.put("to", Players.locationMap(e.getTo()));
        return e.isCancelled();
    }

    private static boolean spawn(Bridge b, Map<String, Object> f) {
        Entity en = Entities.of(b, Wire.map(f.get("entity")));
        if (en == null) return false;
        EntitySpawnEvent e;
        if (en instanceof LivingEntity le) {
            CreatureSpawnEvent.SpawnReason r;
            try {
                r = CreatureSpawnEvent.SpawnReason.valueOf(Wire.str(f, "reason"));
            } catch (IllegalArgumentException ex) {
                r = CreatureSpawnEvent.SpawnReason.DEFAULT;
            }
            e = new CreatureSpawnEvent(le, r);
        } else {
            e = new EntitySpawnEvent(en);
        }
        b.pluginManager.callEvent(e);
        return e.isCancelled();
    }
}
