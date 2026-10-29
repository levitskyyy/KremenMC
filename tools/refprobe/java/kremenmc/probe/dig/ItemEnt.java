package kremenmc.probe.dig;

import java.nio.file.*;
import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.Items;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;

/**
 * Item entity oracle. Input lines:
 *   traj|id|floor|water|x|y|z|vx|vy|vz|ticks  -> per tick position, velocity, onGround
 *   merge|id|countA|countB|dx|ticks           -> survivors after ticking two stone stacks
 *   pop|id|seed|count                         -> Block.popResource with level.random seeded
 */
public final class ItemEnt {
    static ServerLevel level;

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        level = server.overworld();
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        server.submit(() -> {
            level.getChunk(0, 0);
            Ref.out.println("random " + level.getRandom().getClass().getSimpleName());
            for (String line : lines) {
                String[] f = line.strip().split("\\|", -1);
                try {
                    switch (f[0]) {
                        case "traj" -> traj(f);
                        case "merge" -> merge(f);
                        case "pop" -> pop(f);
                        default -> {}
                    }
                } catch (Throwable e) {
                    Ref.out.println("error " + f[1] + " " + e);
                }
            }
            return null;
        }).get();
        Ref.exit();
    }

    static void reset(BlockState floor, boolean water) {
        for (ItemEntity e : level.getEntitiesOfClass(ItemEntity.class, new AABB(-8, 80, -8, 9, 120, 9))) e.discard();
        for (int x = -4; x <= 5; x++)
            for (int y = 96; y <= 106; y++)
                for (int z = -4; z <= 4; z++) {
                    BlockState s = y == 99 ? floor : (water && (y == 100 || y == 101) ? Blocks.WATER.defaultBlockState() : Blocks.AIR.defaultBlockState());
                    if (y < 99) s = Blocks.STONE.defaultBlockState();
                    level.setBlock(new BlockPos(x, y, z), s, 2 | 16);
                }
    }

    static void step(ItemEntity e) {
        e.setOldPosAndRot();
        e.tickCount++;
        e.tick();
    }

    static String v(double d) { return Double.toString(d); }

    static void traj(String[] f) {
        Block fb = BuiltInRegistries.BLOCK.getValue(Identifier.parse(f[2]));
        reset(fb.defaultBlockState(), f[3].equals("1"));
        ItemEntity e = new ItemEntity(level, Double.parseDouble(f[4]), Double.parseDouble(f[5]), Double.parseDouble(f[6]), new ItemStack(Items.STONE),
            Double.parseDouble(f[7]), Double.parseDouble(f[8]), Double.parseDouble(f[9]));
        level.addFreshEntity(e);
        StringBuilder sb = new StringBuilder("traj " + f[1] + " id=" + e.getId());
        int n = Integer.parseInt(f[10]);
        for (int i = 0; i < n; i++) {
            step(e);
            var p = e.position();
            var d = e.getDeltaMovement();
            sb.append(' ').append(v(p.x)).append(',').append(v(p.y)).append(',').append(v(p.z)).append(',')
              .append(v(d.x)).append(',').append(v(d.y)).append(',').append(v(d.z)).append(',').append(e.onGround() ? 1 : 0);
        }
        e.discard();
        Ref.out.println(sb);
    }

    static void merge(String[] f) {
        reset(Blocks.STONE.defaultBlockState(), false);
        ItemEntity a = new ItemEntity(level, 0.5, 100, 0.5, new ItemStack(Items.STONE, Integer.parseInt(f[2])), 0, 0, 0);
        ItemEntity b = new ItemEntity(level, 0.5 + Double.parseDouble(f[4]), 100, 0.5, new ItemStack(Items.STONE, Integer.parseInt(f[3])), 0, 0, 0);
        a.setPickUpDelay(10);
        b.setPickUpDelay(30);
        level.addFreshEntity(a);
        level.addFreshEntity(b);
        int n = Integer.parseInt(f[5]);
        StringBuilder sb = new StringBuilder("merge " + f[1] + " ids=" + a.getId() + "," + b.getId());
        for (int i = 0; i < n; i++) {
            if (!a.isRemoved()) step(a);
            if (!b.isRemoved()) step(b);
            String s = (a.isRemoved() ? "x" : a.getItem().getCount() + ":" + a.getAge()) + "/" + (b.isRemoved() ? "x" : b.getItem().getCount() + ":" + b.getAge());
            sb.append(' ').append(s);
            if (a.isRemoved() || b.isRemoved()) {
                ItemEntity s2 = a.isRemoved() ? b : a;
                var fl = ItemEntity.class.getDeclaredFields();
                sb.append(" pos=").append(v(s2.getX())).append(',').append(v(s2.getY())).append(',').append(v(s2.getZ()));
                try {
                    var pd = ItemEntity.class.getDeclaredField("pickupDelay");
                    pd.setAccessible(true);
                    sb.append(" delay=").append(pd.get(s2));
                } catch (Exception ex) {
                    sb.append(" delay=?");
                }
                break;
            }
        }
        a.discard();
        b.discard();
        Ref.out.println(sb);
    }

    static void pop(String[] f) throws Exception {
        reset(Blocks.STONE.defaultBlockState(), false);
        level.getRandom().setSeed(Long.parseLong(f[2]));
        Block.popResource(level, new BlockPos(1, 100, 0), new ItemStack(Items.DIRT, Integer.parseInt(f[3])));
        for (ItemEntity e : level.getEntitiesOfClass(ItemEntity.class, new AABB(-8, 80, -8, 9, 120, 9))) {
            var d = e.getDeltaMovement();
            var pd = ItemEntity.class.getDeclaredField("pickupDelay");
            pd.setAccessible(true);
            Ref.out.println("pop " + f[1] + " " + v(e.getX()) + "," + v(e.getY()) + "," + v(e.getZ()) + "," + v(d.x) + "," + v(d.y) + "," + v(d.z)
                + " yrot=" + e.getYRot() + " delay=" + pd.get(e) + " next=" + level.getRandom().nextLong());
            e.discard();
        }
    }
}
