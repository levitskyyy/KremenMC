package kremenmc.probe.explosion;

import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.BlockHitResult;
import net.minecraft.world.phys.Vec3;

// Portal checks nether frame sizes and end ring facing black-box: it
// builds frames with commands and uses flint and steel / an eye of ender
// through a fake survival player.
public final class Portal {
    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> { Env.command("gamerule spawn_mobs false"); Env.command("forceload add -160 -16 60 60"); });
        Env.waitTicks(80);
        int[][] sizes = {{2, 3, 1}, {2, 3, 0}, {1, 3, 1}, {2, 2, 1}, {21, 21, 1}, {22, 3, 1}, {3, 22, 1}, {21, 3, 0}};
        int n = 0;
        for (int[] s : sizes) {
            int x0 = -120 + 30 * n++, y0 = 10, z0 = 4;
            int w = s[0], h = s[1];
            boolean corners = s[2] == 1;
            Env.run(() -> {
                load(x0 - 2, z0 - 2, x0 + w + 2, z0 + 2);
                Env.command(String.format("fill %d %d %d %d %d %d air", x0 - 2, y0 - 1, z0 - 1, x0 + w + 1, y0 + h + 1, z0 + 1));
                for (int i = -1; i <= w; i++)
                    for (int k = -1; k <= h; k++) {
                        boolean side = i == -1 || i == w, cap = k == -1 || k == h;
                        if (side && cap && !corners) continue;
                        if (side || cap) Env.command(String.format("setblock %d %d %d obsidian", x0 + i, y0 + k, z0));
                    }
                use("flint_and_steel", new BlockPos(x0, y0 - 1, z0), Direction.UP);
                int portals = 0;
                String axis = "-";
                for (int i = 0; i < w; i++)
                    for (int k = 0; k < h; k++) {
                        var st = Env.level.getBlockState(new BlockPos(x0 + i, y0 + k, z0));
                        if (st.is(net.minecraft.world.level.block.Blocks.NETHER_PORTAL)) {
                            portals++;
                            axis = st.getValue(net.minecraft.world.level.block.state.properties.BlockStateProperties.HORIZONTAL_AXIS).getName();
                        }
                    }
                o.println("frame " + w + "x" + h + " corners=" + corners + " portals=" + portals + " axis=" + axis);
            });
        }
        for (String mode : new String[]{"inward", "outward"}) {
            int cx = (mode.equals("inward") ? 0 : 20), cy = 10, cz = 30;
            Env.run(() -> {
                load(cx - 3, cz - 3, cx + 3, cz + 3);
                int[][] ring = new int[12][2];
                String[] facing = new String[12];
                int m = 0;
                for (int i = -1; i <= 1; i++) {
                    ring[m] = new int[]{i, -2}; facing[m++] = "south";
                    ring[m] = new int[]{i, 2}; facing[m++] = "north";
                    ring[m] = new int[]{-2, i}; facing[m++] = "east";
                    ring[m] = new int[]{2, i}; facing[m++] = "west";
                }
                for (int k = 0; k < 12; k++) {
                    String f = facing[k];
                    if (mode.equals("outward")) f = Direction.byName(f).getOpposite().getName();
                    Env.command(String.format("setblock %d %d %d end_portal_frame[facing=%s,eye=%s]", cx + ring[k][0], cy, cz + ring[k][1], f, k == 0 ? "false" : "true"));
                }
                use("ender_eye", new BlockPos(cx + ring[0][0], cy, cz + ring[0][1]), Direction.UP);
                int portals = 0;
                for (int dx = -1; dx <= 1; dx++)
                    for (int dz = -1; dz <= 1; dz++)
                        if (Env.level.getBlockState(new BlockPos(cx + dx, cy, cz + dz)).is(net.minecraft.world.level.block.Blocks.END_PORTAL)) portals++;
                o.println("ring " + mode + " end_portals=" + portals);
            });
        }
        for (String fl : Env.failures) o.println("fail " + fl);
        Env.exit();
    }

    static void load(int x0, int z0, int x1, int z1) {
        for (int x = x0 >> 4; x <= x1 >> 4; x++)
            for (int z = z0 >> 4; z <= z1 >> 4; z++) Env.level.getChunk(x, z);
    }

    static void use(String item, BlockPos bp, Direction face) {
        FakePlayer p = new FakePlayer(Env.level, 70);
        p.snapTo(bp.getX() + 0.5, bp.getY() + 1, bp.getZ() - 2.5, 0, 0);
        Env.level.addNewPlayer(p);
        p.setGameMode(GameType.SURVIVAL);
        var stack = new ItemStack(BuiltInRegistries.ITEM.getValue(Identifier.parse(item)), 1);
        p.setItemInHand(InteractionHand.MAIN_HAND, stack);
        var hit = new BlockHitResult(Vec3.atCenterOf(bp).add(face.getStepX() * 0.5, face.getStepY() * 0.5, face.getStepZ() * 0.5), face, bp, false);
        p.gameMode.useItemOn(p, Env.level, stack, InteractionHand.MAIN_HAND, hit);
        Env.level.removePlayerImmediately(p, Entity.RemovalReason.DISCARDED);
    }
}
