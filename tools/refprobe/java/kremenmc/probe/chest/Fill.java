package kremenmc.probe.chest;

import java.lang.reflect.Method;
import java.util.*;
import it.unimi.dsi.fastutil.objects.ObjectArrayList;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.util.RandomSource;
import net.minecraft.world.Container;
import net.minecraft.world.SimpleContainer;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.entity.RandomizableContainerBlockEntity;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;
import net.minecraft.world.level.storage.loot.LootParams;
import net.minecraft.world.level.storage.loot.LootTable;
import net.minecraft.world.level.storage.loot.parameters.LootContextParamSets;
import net.minecraft.world.level.storage.loot.parameters.LootContextParams;
import net.minecraft.world.phys.Vec3;

/**
 * Chest loot fill oracle. Args: table seed...
 * For each seed prints
 *   real <seed> <slot>:<item> ...   (a real chest block entity, unpackLootTable(null))
 *   roll <seed> <item> ...          (getRandomItems with LegacyRandomSource(seed))
 *   slots <seed> <list> | <draws>   (private getAvailableSlots on an empty 27-slot container)
 *   split <seed> <items> | <draws>  (private shuffleAndSplitItems)
 */
public final class Fill {
    static final class LogRand implements RandomSource {
        final RandomSource d;
        StringBuilder log = new StringBuilder();
        LogRand(RandomSource d) { this.d = d; }
        void add(String s) { if (log.length() > 0) log.append(' '); log.append(s); }
        public RandomSource fork() { add("fork"); return d.fork(); }
        public PositionalRandomFactory forkPositional() { add("forkPos"); return d.forkPositional(); }
        public void setSeed(long s) { d.setSeed(s); }
        public int nextInt() { int v = d.nextInt(); add("I=" + v); return v; }
        public int nextInt(int b) { int v = d.nextInt(b); add("i" + b + "=" + v); return v; }
        public long nextLong() { long v = d.nextLong(); add("L=" + v); return v; }
        public boolean nextBoolean() { boolean v = d.nextBoolean(); add("b=" + v); return v; }
        public float nextFloat() { float v = d.nextFloat(); add("f=" + v); return v; }
        public double nextDouble() { double v = d.nextDouble(); add("d=" + v); return v; }
        public double nextGaussian() { double v = d.nextGaussian(); add("g=" + v); return v; }
    }

    static String it(ItemStack s) {
        String r = BuiltInRegistries.ITEM.getKey(s.getItem()).getPath() + "*" + s.getCount();
        if (!s.getComponentsPatch().isEmpty()) r += "+" + s.getComponentsPatch().toString().replace(' ', '_');
        return r;
    }

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        ServerLevel level = server.overworld();
        var key = ResourceKey.create(Registries.LOOT_TABLE, Identifier.parse(args[0]));
        server.submit(() -> {
          try {
            level.getChunk(0, 0);
            BlockPos pos = new BlockPos(0, 100, 0);
            LootTable table = level.getServer().reloadableRegistries().getLootTable(key);
            Method slotsM = LootTable.class.getDeclaredMethod("getAvailableSlots", Container.class, RandomSource.class);
            Method splitM = LootTable.class.getDeclaredMethod("shuffleAndSplitItems", ObjectArrayList.class, int.class, RandomSource.class);
            slotsM.setAccessible(true);
            splitM.setAccessible(true);
            for (int a = 1; a < args.length; a++) {
                long seed = Long.parseLong(args[a]);
                level.setBlock(pos, Blocks.AIR.defaultBlockState(), 2);
                level.setBlock(pos, Blocks.CHEST.defaultBlockState(), 2);
                var be = (RandomizableContainerBlockEntity) level.getBlockEntity(pos);
                be.setLootTable(key, seed);
                be.unpackLootTable(null);
                StringBuilder sb = new StringBuilder("real " + seed);
                for (int i = 0; i < be.getContainerSize(); i++) {
                    if (!be.getItem(i).isEmpty()) sb.append(' ').append(i).append(':').append(it(be.getItem(i)));
                }
                Ref.out.println(sb);

                var params = new LootParams.Builder(level).withParameter(LootContextParams.ORIGIN, Vec3.atCenterOf(pos)).create(LootContextParamSets.CHEST);
                LogRand r = new LogRand(new LegacyRandomSource(seed));
                ObjectArrayList<ItemStack> items = table.getRandomItems(params, r);
                sb = new StringBuilder("roll " + seed);
                for (ItemStack s : items) sb.append(' ').append(it(s));
                Ref.out.println(sb);
                r.log = new StringBuilder();
                @SuppressWarnings("unchecked")
                List<Integer> slots = (List<Integer>) slotsM.invoke(table, new SimpleContainer(27), r);
                Ref.out.println("slots " + seed + " " + slots + " | " + r.log);
                r.log = new StringBuilder();
                splitM.invoke(table, items, slots.size(), r);
                sb = new StringBuilder("split " + seed);
                for (ItemStack s : items) sb.append(' ').append(it(s));
                Ref.out.println(sb + " | " + r.log);
            }
          } catch (Exception e) {
            throw new RuntimeException(e);
          }
            return null;
        }).get();
        Ref.exit();
    }
}
