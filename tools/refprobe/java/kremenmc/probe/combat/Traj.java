package kremenmc.probe.combat;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.TicketType;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.item.FallingBlockEntity;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.phys.Vec3;

// Traj spawns an entity with a given velocity in a controlled world, ticks it
// by hand (the level is frozen) and prints position, velocity and on-ground
// after every tick. Script lines:
//   setup <command>
//   case <name> <type> x y z vx vy vz ticks [noai] [yaw]
// type "falling_block:<block>" and "item:<item>" are special.
public final class Traj {
    public static void main(String[] args) throws Exception {
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule random_tick_speed 0");
            for (int cx = -4; cx <= 4; cx++)
                for (int cz = -4; cz <= 4; cz++) Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos(cx, cz), 2);
        });
        Env.waitTicks(40);
        for (String raw : lines) {
            String line = raw.strip();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] f = line.split("\\s+");
            if (f[0].equals("setup")) {
                String cmd = line.substring(5).strip();
                Env.run(() -> Env.command(cmd));
            }
        }
        Env.waitTicks(5);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String fl : Env.failures) o.println("warn " + fl);
        for (String raw : lines) {
            String line = raw.strip();
            if (!line.startsWith("case ")) continue;
            String[] f = line.split("\\s+");
            String res = Env.call(() -> runCase(f));
            o.print(res);
        }
        Env.exit();
    }

    static String fmt(Entity e) {
        Vec3 p = e.position(), v = e.getDeltaMovement();
        return p.x + " " + p.y + " " + p.z + " " + v.x + " " + v.y + " " + v.z + " " + (e.onGround() ? 1 : 0) + (e.isRemoved() ? " removed" : "");
    }

    // stripAi removes goals and brain behaviours so the mob keeps physics but
    // never moves on its own.
    static void stripAi(Mob m) {
        try {
            for (String fn : new String[] {"goalSelector", "targetSelector"}) {
                var f = Mob.class.getDeclaredField(fn);
                f.setAccessible(true);
                ((net.minecraft.world.entity.ai.goal.GoalSelector) f.get(m)).removeAllGoals(g -> true);
            }
            m.getBrain().removeAllBehaviors();
        } catch (ReflectiveOperationException ex) {
            throw new RuntimeException(ex);
        }
    }

    static String runCase(String[] f) {
        StringBuilder sb = new StringBuilder();
        var level = Env.level;
        String name = f[1], type = f[2];
        double x = Double.parseDouble(f[3]), y = Double.parseDouble(f[4]), z = Double.parseDouble(f[5]);
        Vec3 v = new Vec3(Double.parseDouble(f[6]), Double.parseDouble(f[7]), Double.parseDouble(f[8]));
        int n = Integer.parseInt(f[9]);
        boolean noai = f.length > 10 && f[10].equals("noai");
        Entity e;
        if (type.startsWith("falling_block:")) {
            var b = BuiltInRegistries.BLOCK.getValue(Identifier.parse(type.substring(14)));
            BlockPos bp = BlockPos.containing(x, y, z);
            level.setBlock(bp, b.defaultBlockState(), 3);
            e = FallingBlockEntity.fall(level, bp, b.defaultBlockState());
            e.setDeltaMovement(v);
        } else if (type.startsWith("item:")) {
            var it = BuiltInRegistries.ITEM.getValue(Identifier.parse(type.substring(5)));
            e = new ItemEntity(level, x, y, z, new ItemStack(it), v.x, v.y, v.z);
            level.addFreshEntity(e);
        } else {
            var t = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(type));
            e = t.create(level, EntitySpawnReason.COMMAND);
            e.snapTo(x, y, z, 0, 0);
            e.setDeltaMovement(v);
            if (noai && e instanceof Mob m) stripAi(m);
            level.addFreshEntity(e);
        }
        sb.append("case ").append(name).append(" 0 ").append(fmt(e)).append('\n');
        for (int i = 1; i <= n && !e.isRemoved(); i++) {
            level.tickNonPassenger(e);
            sb.append("case ").append(name).append(' ').append(i).append(' ').append(fmt(e)).append('\n');
        }
        e.discard();
        return sb.toString();
    }
}
