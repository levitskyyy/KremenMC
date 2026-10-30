package kremenmc.probe.fluid;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;

// Scenes runs fluid scenes from a script on the frozen reference server
// and prints the block states of the scene area tick by tick. Black-box
// only: blocks are placed with commands and read back with getBlockState.
//
//	scene <name> <overworld|nether> x0 y0 z0 x1 y1 z1   clear and watch an area
//	cmd <command>                                        run a command in the scene dimension
//	run <n>                                              step n ticks, printing changes
//
// Output: "scene <name>", "s x y z <state>" for the non-air area at the
// start of each run, then "t <tick> x y z <state>" for every change.
public final class Scenes {
    static ServerLevel level;
    static int x0, y0, z0, x1, y1, z1, tick;
    static Map<Long, String> prev = new HashMap<>();
    static List<FakePlayer> players = new ArrayList<>();
    static List<net.minecraft.world.entity.Entity> ents = new ArrayList<>();

    public static void main(String[] args) throws Exception {
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule advance_time false");
            Env.command("gamerule advance_weather false");
            Env.command("gamerule max_block_modifications 10000000");
        });
        Env.waitTicks(5);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String raw : lines) {
            String line = raw.strip();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] f = line.split("\\s+");
            if (f[0].equals("run")) {
                int n = Integer.parseInt(f[1]);
                o.print(Env.call(Scenes::snap));
                for (int i = 0; i < n; i++) {
                    long gt = Env.call(() -> level.getGameTime());
                    Env.run(() -> Env.server.tickRateManager().stepGameIfPaused(1));
                    for (int k = 0; k < 4000 && Env.call(() -> level.getGameTime()) == gt; k++) Thread.sleep(1);
                    // let the stepped tick finish before reading
                    Env.call(() -> 0);
                    tick++;
                    long now = Env.call(() -> level.getGameTime());
                    if (now != gt + 1) o.println("warn gametime " + gt + " -> " + now);
                    o.print(Env.call(Scenes::diff));
                    o.print(Env.call(() -> {
                        StringBuilder eb = new StringBuilder();
                        for (var e : ents) {
                            var v = e.getDeltaMovement();
                            eb.append("e ").append(tick).append(' ').append(e.getX()).append(' ').append(e.getY()).append(' ').append(e.getZ())
                              .append(' ').append(v.x).append(' ').append(v.y).append(' ').append(v.z).append('\n');
                        }
                        return eb.toString();
                    }));
                }
                continue;
            }
            String res = Env.call(() -> op(f, line));
            o.print(res);
            for (String w : Env.failures) o.println("warn " + w);
            Env.failures.clear();
        }
        Env.exit();
    }

    static String op(String[] f, String line) {
        StringBuilder sb = new StringBuilder();
        switch (f[0]) {
            case "scene" -> {
                level = f[2].equals("nether") ? Env.server.getLevel(Level.NETHER) : Env.server.overworld();
                x0 = Integer.parseInt(f[3]); y0 = Integer.parseInt(f[4]); z0 = Integer.parseInt(f[5]);
                x1 = Integer.parseInt(f[6]); y1 = Integer.parseInt(f[7]); z1 = Integer.parseInt(f[8]);
                cmd(String.format("forceload add %d %d %d %d", x0 - 16, z0 - 16, x1 + 16, z1 + 16));
                cmd(String.format("fill %d %d %d %d %d %d air", x0 - 1, y0 - 1, z0 - 1, x1 + 1, y1 + 1, z1 + 1));
                tick = 0;
                prev.clear();
                for (FakePlayer fp : players) fp.level().removePlayerImmediately(fp, net.minecraft.world.entity.Entity.RemovalReason.DISCARDED);
                players.clear();
                FakePlayer fp = new FakePlayer(level, 0);
                fp.snapTo((x0 + x1) / 2.0, y1 + 8, (z0 + z1) / 2.0, 0, 0);
                fp.setGameMode(net.minecraft.world.level.GameType.SPECTATOR);
                level.addNewPlayer(fp);
                fp.sent();
                players.add(fp);
                sb.append("scene ").append(f[1]).append('\n');
            }
            case "cmd" -> cmd(line.substring(4));
            case "item" -> {
                var it = new net.minecraft.world.entity.item.ItemEntity(level, Double.parseDouble(f[1]), Double.parseDouble(f[2]), Double.parseDouble(f[3]),
                    new net.minecraft.world.item.ItemStack(net.minecraft.world.item.Items.STONE));
                it.setDeltaMovement(0, 0, 0);
                it.setPickUpDelay(32767);
                level.addFreshEntity(it);
                ents.add(it);
            }
            case "dbg" -> {
                BlockPos p = new BlockPos(Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                sb.append("dbg gt=").append(level.getGameTime()).append(" sched=")
                    .append(level.getFluidTicks().hasScheduledTick(p, level.getFluidState(p).getType()))
                    .append(" blocks=").append(level.shouldTickBlocksAt(net.minecraft.world.level.ChunkPos.containing(p).pack()))
                    .append(" ent=").append(level.isPositionEntityTicking(p)).append('\n');
            }
            default -> throw new IllegalArgumentException(line);
        }
        return sb.toString();
    }

    static void cmd(String c) {
        var src = Env.server.createCommandSourceStack().withLevel(level).withSource(new net.minecraft.commands.CommandSource() {
            public void sendSystemMessage(net.minecraft.network.chat.Component m) { Env.failures.add(c + ": " + m.getString()); }
            public boolean acceptsSuccess() { return false; }
            public boolean acceptsFailure() { return true; }
            public boolean shouldInformAdmins() { return false; }
        });
        Env.server.getCommands().performPrefixedCommand(src, c);
    }

    static String snap() {
        StringBuilder sb = new StringBuilder();
        prev.clear();
        for (int y = y0; y <= y1; y++)
            for (int z = z0; z <= z1; z++)
                for (int x = x0; x <= x1; x++) {
                    String s = state(level.getBlockState(new BlockPos(x, y, z)));
                    prev.put(BlockPos.asLong(x, y, z), s);
                    if (!s.equals("air")) sb.append("s ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(s).append('\n');
                }
        return sb.toString();
    }

    static String diff() {
        StringBuilder sb = new StringBuilder();
        for (int y = y0; y <= y1; y++)
            for (int z = z0; z <= z1; z++)
                for (int x = x0; x <= x1; x++) {
                    String s = state(level.getBlockState(new BlockPos(x, y, z)));
                    String p = prev.put(BlockPos.asLong(x, y, z), s);
                    if (!s.equals(p)) sb.append("t ").append(tick).append(' ').append(x).append(' ').append(y).append(' ').append(z).append(' ').append(s).append('\n');
                }
        return sb.toString();
    }

    static String state(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath());
        String sep = "[";
        for (Property<?> p : s.getProperties()) {
            b.append(sep).append(p.getName()).append('=').append(s.getValue(p).toString().toLowerCase());
            sep = ",";
        }
        if (!sep.equals("[")) b.append(']');
        return b.toString();
    }
}
