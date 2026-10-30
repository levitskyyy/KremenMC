package kremenmc.probe.ai;

import java.util.*;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.TicketType;
import net.minecraft.world.Difficulty;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.ai.goal.GoalSelector;
import net.minecraft.world.entity.ai.goal.WrappedGoal;
import net.minecraft.world.entity.monster.Creeper;
import net.minecraft.world.entity.projectile.arrow.AbstractArrow;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.AABB;

// Observe is a black-box recorder of mob behaviour: it dumps the goal lists
// of a set of mob types and records position, target and attacks per tick
// in a few controlled scenes on stone platforms at y=100 (night, normal).
public final class Observe {
    static Runnable active;

    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule advance_time false");
            Env.command("gamerule do_daylight_cycle false");
            Env.command("gamerule advance_weather false");
            Env.command("time set 18000");
            Env.server.setDifficulty(Difficulty.NORMAL, true);
            for (int s = 0; s < 6; s++)
                for (int cx = -3; cx <= 3; cx++)
                    for (int cz = -3; cz <= 3; cz++)
                        Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos((s * 300 >> 4) + cx, cz), 2);
            Env.server.addTickable(() -> {
                if (active != null) active.run();
            });
        });
        Env.waitTicks(60);
        Env.run(() -> {
            for (int s = 0; s < 6; s++) {
                int x = s * 300;
                Env.command("fill " + (x - 24) + " 100 -24 " + (x + 24) + " 100 24 minecraft:stone");
            }
        });
        Env.waitTicks(5);
        for (String fl : Env.failures) o.println("warn " + fl);
        if (args.length > 0) Env.run(() -> {
            for (String t : new String[] {"cow", "pig", "sheep", "chicken", "rabbit", "zombie", "husk", "drowned", "zombie_villager", "skeleton", "stray", "creeper", "spider", "cave_spider", "enderman", "slime", "witch", "wolf", "iron_golem"})
                goals(t);
        });
        if (args.length == 0 || args[0].equals("chase")) {
            chase(0);
            skeleton(300);
            creeper(600);
        }
        wander(900);
        Env.exit();
    }

    static Mob create(String type, double x, double y, double z) {
        var t = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse("minecraft:" + type));
        Mob m = (Mob) t.create(Env.level, EntitySpawnReason.COMMAND);
        m.snapTo(x, y, z, 0, 0);
        return m;
    }

    static void goals(String type) throws Exception {
        Mob m = create(type, 0, 101, 0);
        dump(type, "g", m.getGoalSelector());
        var f = Mob.class.getDeclaredField("targetSelector");
        f.setAccessible(true);
        dump(type, "t", (GoalSelector) f.get(m));
        Env.out.println("attr " + type + " speed " + m.getAttributeValue(net.minecraft.world.entity.ai.attributes.Attributes.MOVEMENT_SPEED)
                + " follow " + m.getAttributeValue(net.minecraft.world.entity.ai.attributes.Attributes.FOLLOW_RANGE));
    }

    static void dump(String type, String kind, GoalSelector s) {
        List<WrappedGoal> gs = new ArrayList<>(s.getAvailableGoals());
        gs.sort(Comparator.comparingInt(WrappedGoal::getPriority));
        for (WrappedGoal g : gs)
            Env.out.println("goal " + type + " " + kind + " " + g.getPriority() + " " + g.getGoal().getClass().getSimpleName() + " " + g.getFlags());
    }

    static FakePlayer player(double x, double z) {
        FakePlayer fp = new FakePlayer(Env.level, "p" + (int) x);
        fp.setGameMode(GameType.SURVIVAL);
        fp.snapTo(x, 101, z, 90, 0);
        Env.level.addNewPlayer(fp);
        return fp;
    }

    static String running(Mob m) {
        StringBuilder sb = new StringBuilder();
        for (WrappedGoal g : m.getGoalSelector().getAvailableGoals())
            if (g.isRunning()) sb.append(g.getGoal().getClass().getSimpleName()).append(',');
        return sb.length() == 0 ? "-" : sb.toString();
    }

    static void runFor(int ticks, Runnable r) throws Exception {
        int[] n = {0};
        Env.run(() -> active = () -> {
            if (n[0]++ < ticks) r.run();
        });
        Env.waitTicks(ticks + 2);
        Env.run(() -> active = null);
    }

    static String f(double v) {
        return String.format(Locale.ROOT, "%.4f", v);
    }

    static void chase(int x) throws Exception {
        FakePlayer[] p = new FakePlayer[1];
        Mob[] z = new Mob[1];
        Env.run(() -> {
            p[0] = player(x + 0.5, 0.5);
            z[0] = create("zombie", x + 14.5, 101, 0.5);
            Env.level.addFreshEntity(z[0]);
        });
        int[] t = {0};
        runFor(240, () -> {
            var zm = z[0];
            p[0].doTick();
            t[0]++;
            var v = zm.getDeltaMovement();
            String hit = "";
            if (p[0].getHealth() < 20) {
                hit = " HIT " + f(20 - p[0].getHealth());
                p[0].setHealth(20);
            }
            Env.out.println("chase " + t[0] + " " + f(zm.getX() - x) + " " + f(zm.getY()) + " " + f(zm.getZ()) + " " + f(Math.hypot(v.x, v.z)) + " tgt=" + (zm.getTarget() != null) + " " + running(zm) + hit);
        });
        Env.run(() -> {
            z[0].discard();
            Env.level.removePlayerImmediately(p[0], Entity.RemovalReason.DISCARDED);
        });
    }

    static void skeleton(int x) throws Exception {
        FakePlayer[] p = new FakePlayer[1];
        Mob[] s = new Mob[1];
        Env.run(() -> {
            p[0] = player(x + 0.5, 0.5);
            s[0] = create("skeleton", x + 12.5, 101, 0.5);
            s[0].setItemSlot(net.minecraft.world.entity.EquipmentSlot.MAINHAND, new net.minecraft.world.item.ItemStack(net.minecraft.world.item.Items.BOW));
            Env.level.addFreshEntity(s[0]);
            ((net.minecraft.world.entity.monster.skeleton.AbstractSkeleton) s[0]).reassessWeaponGoal();
        });
        int[] t = {0};
        runFor(400, () -> {
            var sk = s[0];
            p[0].doTick();
            p[0].setHealth(20);
            t[0]++;
            String shot = "";
            for (Entity e : Env.level.getEntities((Entity) null, new AABB(x - 30, 90, -30, x + 30, 120, 30), e -> e instanceof AbstractArrow)) {
                shot = " SHOT";
                e.discard();
            }
            Env.out.println("skel " + t[0] + " " + f(sk.getX() - x) + " " + f(sk.getZ()) + " using=" + sk.isUsingItem() + " " + running(sk) + shot);
        });
        Env.run(() -> {
            s[0].discard();
            Env.level.removePlayerImmediately(p[0], Entity.RemovalReason.DISCARDED);
        });
    }

    static void creeper(int x) throws Exception {
        FakePlayer[] p = new FakePlayer[1];
        Creeper[] c = new Creeper[1];
        Env.run(() -> {
            Env.command("gamerule mob_griefing false");
            p[0] = player(x + 0.5, 0.5);
            c[0] = (Creeper) create("creeper", x + 8.5, 101, 0.5);
            Env.level.addFreshEntity(c[0]);
        });
        int[] t = {0};
        runFor(200, () -> {
            var cr = c[0];
            p[0].doTick();
            t[0]++;
            String gone = cr.isRemoved() ? " REMOVED" : "";
            float hp = p[0].getHealth();
            if (hp < 20) p[0].setHealth(20);
            if (!cr.isRemoved() || t[0] < 200)
                Env.out.println("creeper " + t[0] + " " + f(cr.getX() - x) + " swell=" + f(cr.getSwelling(1f)) + " dir=" + cr.getSwellDir() + " " + running(cr) + gone + (hp < 20 ? " HIT " + f(20 - hp) : ""));
        });
        Env.run(() -> Env.level.removePlayerImmediately(p[0], Entity.RemovalReason.DISCARDED));
    }

    static void wander(int x) throws Exception {
        List<Mob> cows = new ArrayList<>();
        String[] types = {"cow", "cow", "pig", "sheep", "chicken", "zombie", "skeleton", "creeper"};
        Env.run(() -> {
            FakePlayer fp = new FakePlayer(Env.level, "watcher");
            fp.setGameMode(GameType.CREATIVE);
            fp.snapTo(x + 0.5, 101, 19.5, 0, 0);
            Env.level.addNewPlayer(fp);
            for (int i = 0; i < 8; i++) {
                Mob m = create(types[i], x - 12 + (i % 4) * 8 + 0.5, 101, (i < 4 ? -8 : 8) + 0.5);
                Env.level.addFreshEntity(m);
                cows.add(m);
            }
        });
        int[] t = {0};
        double[][] last = new double[8][2];
        boolean[] moving = new boolean[8];
        int[] start = new int[8];
        runFor(2400, () -> {
            t[0]++;
            for (int i = 0; i < cows.size(); i++) {
                Mob m = cows.get(i);
                double dx = m.getX() - last[i][0], dz = m.getZ() - last[i][1];
                last[i][0] = m.getX();
                last[i][1] = m.getZ();
                boolean mv = t[0] > 1 && Math.hypot(dx, dz) > 0.003;
                if (mv && !moving[i]) start[i] = t[0];
                if (!mv && moving[i]) Env.out.println("wander " + m.getType().toShortString() + i + " " + start[i] + " " + t[0] + " " + running(m));
                if (mv && t[0] % 10 == 0) Env.out.println("wspeed " + m.getType().toShortString() + i + " " + t[0] + " " + f(Math.hypot(dx, dz)));
                moving[i] = mv;
            }
        });
    }
}
