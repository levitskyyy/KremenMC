package kremenmc.probe.combat;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.TicketType;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.projectile.Projectile;
import net.minecraft.world.entity.projectile.arrow.AbstractArrow;
import net.minecraft.world.entity.projectile.arrow.Arrow;
import net.minecraft.world.entity.projectile.arrow.ThrownTrident;
import net.minecraft.world.entity.projectile.throwableitemprojectile.Snowball;
import net.minecraft.world.entity.projectile.throwableitemprojectile.ThrownEgg;
import net.minecraft.world.entity.projectile.throwableitemprojectile.ThrownEnderpearl;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.Items;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.Vec3;

// Proj fires projectiles owned by a fake player at a target in a frozen
// level, ticking projectile and target by hand, and prints what happened.
//   shot <name> <arrow|trident|snowball|egg|ender_pearl> pos=x,y,z vel=x,y,z
//        [crit] [weapon=item+ench@lvl] [target=type@x,y,z] [owner=x,y,z] [n=N] [ticks=T]
public final class Proj {
    public static void main(String[] args) throws Exception {
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            for (int cx = -2; cx <= 2; cx++)
                for (int cz = -2; cz <= 2; cz++) Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos(cx, cz), 2);
        });
        Env.waitTicks(20);
        Env.run(() -> Env.command("fill -8 100 -8 8 100 8 minecraft:stone"));
        Env.waitTicks(2);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String raw : lines) {
            String line = raw.strip();
            if (!line.startsWith("shot ")) continue;
            o.print(Env.call(() -> {
                try {
                    return run(line.split("\\s+"));
                } catch (Throwable t) {
                    return "error " + line + " " + t + "\n";
                }
            }));
        }
        Env.exit();
    }

    static String run(String[] f) throws Exception {
        var level = Env.level;
        Map<String, String> kv = new HashMap<>();
        for (int i = 3; i < f.length; i++) {
            String[] p = f[i].split("=", 2);
            kv.put(p[0], p.length > 1 ? p[1] : "");
        }
        int n = Integer.parseInt(kv.getOrDefault("n", "1"));
        int ticks = Integer.parseInt(kv.getOrDefault("ticks", "40"));
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < n; i++) {
            FakePlayer owner = new FakePlayer(level, "owner");
            owner.setGameMode(GameType.SURVIVAL);
            Vec3 op = Hits.vec(kv.getOrDefault("owner", "0.5,101,-6.5"));
            owner.snapTo(op.x, op.y, op.z, 0, 0);
            LivingEntity target = null;
            if (kv.containsKey("target")) {
                String[] t = kv.get("target").split("@");
                Vec3 tp = Hits.vec(t[1]);
                target = Hits.spawn(t[0], tp, 180);
            }
            ItemStack weapon = kv.containsKey("weapon") ? Hits.stack(kv.get("weapon")) : new ItemStack(Items.BOW);
            Projectile pr = switch (f[2]) {
                case "arrow" -> new Arrow(level, owner, new ItemStack(Items.ARROW), weapon);
                case "trident" -> new ThrownTrident(level, owner, new ItemStack(Items.TRIDENT));
                case "snowball" -> new Snowball(level, owner, new ItemStack(Items.SNOWBALL));
                case "egg" -> new ThrownEgg(level, owner, new ItemStack(Items.EGG));
                case "ender_pearl" -> new ThrownEnderpearl(level, owner, new ItemStack(Items.ENDER_PEARL));
                default -> throw new IllegalArgumentException(f[2]);
            };
            Vec3 p = Hits.vec(kv.get("pos")), v = Hits.vec(kv.get("vel"));
            pr.snapTo(p.x, p.y, p.z, 0, 0);
            pr.setDeltaMovement(v);
            if (pr instanceof AbstractArrow a && kv.containsKey("crit")) a.setCritArrow(true);
            level.addFreshEntity(pr);
            float hp0 = target != null ? target.getHealth() : 0;
            int k = 0;
            String hitAt = "";
            for (; k < ticks && !pr.isRemoved(); k++) {
                level.tickNonPassenger(pr);
                if (target != null && target.getHealth() != hp0 && hitAt.isEmpty()) {
                    Vec3 tv = target.getDeltaMovement();
                    hitAt = " hit=" + (k + 1) + " tvel=" + tv.x + "," + tv.y + "," + tv.z;
                }
                if (pr instanceof AbstractArrow a && a.getDeltaMovement().lengthSqr() == 0 && !hitAt.isEmpty()) break;
            }
            Vec3 pp = pr.position(), pv = pr.getDeltaMovement();
            sb.append("shot ").append(f[1]).append(' ').append(i)
                .append(" dmg=").append(target != null ? hp0 - target.getHealth() : 0).append(hitAt)
                .append(" ppos=").append(pp.x).append(',').append(pp.y).append(',').append(pp.z)
                .append(" pvel=").append(pv.x).append(',').append(pv.y).append(',').append(pv.z)
                .append(" removed=").append(pr.isRemoved())
                .append(" owner=").append(owner.getX()).append(',').append(owner.getY()).append(',').append(owner.getZ()).append(" ohp=").append(owner.getHealth());
            if (target != null) sb.append(" thp=").append(target.getHealth());
            sb.append('\n');
            if (target != null) target.discard();
            pr.discard();
            for (Entity e : level.getAllEntities()) if (e instanceof Mob m && !(m == target)) m.discard();
        }
        return sb.toString();
    }
}
