package kremenmc.probe.dig;

import java.nio.file.*;
import java.util.*;
import com.mojang.brigadier.StringReader;
import net.minecraft.commands.arguments.blocks.BlockStateParser;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.network.RegistryFriendlyByteBuf;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.util.RandomSource;
import net.minecraft.world.damagesource.DamageSource;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.player.Player;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.storage.loot.LootParams;
import net.minecraft.world.level.storage.loot.LootTable;
import net.minecraft.world.level.storage.loot.parameters.LootContextParamSets;
import net.minecraft.world.level.storage.loot.parameters.LootContextParams;
import net.minecraft.world.phys.Vec3;

/**
 * Loot oracle. Input lines:
 *   tables                                      -> block -> loot table id
 *   block|id|table|state|tool|explosion|seed    -> stacks and the next long of the source after rolling
 *   entity|id|table|type|flags|weapon|byPlayer|damage|seed
 *   seq|worldSeed|name|n                        -> first n longs of level.getRandomSequence(name)
 */
public final class Loot {
    static ServerLevel level;
    static ItemParser items;

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        level = server.overworld();
        items = new ItemParser(server.registryAccess());
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        boolean dig = args.length > 1 && args[1].equals("digest");
        boolean full = System.getenv("KREMENMC_LOOT_FULL") != null;
        Map<String, List<String>> groups = new LinkedHashMap<>();
        server.submit(() -> {
            level.getChunk(0, 0);
            Player killer = new FakePlayer(level);
            for (String line : lines) {
                String[] f = line.strip().split("\\|", -1);
                try {
                    switch (f[0]) {
                        case "tables" -> {
                            for (Block b : BuiltInRegistries.BLOCK) {
                                var t = b.getLootTable();
                                Ref.out.println("table " + BuiltInRegistries.BLOCK.getKey(b).getPath() + " " + (t.isPresent() ? t.get().identifier() : "-"));
                            }
                        }
                        case "block", "entity" -> {
                            String res = f[0].equals("block") ? block(f) : entity(f, killer);
                            if (!dig || full) Ref.out.println(res);
                            groups.computeIfAbsent(f[2], k -> new ArrayList<>()).add(res);
                        }
                        case "seq" -> {
                            var rs = new net.minecraft.world.RandomSequences();
                            var r = rs.get(Identifier.parse(f[2]), Long.parseLong(f[1]));
                            StringBuilder sb = new StringBuilder("seq " + f[1] + " " + f[2]);
                            for (int i = 0; i < Integer.parseInt(f[3]); i++) sb.append(' ').append(r.nextLong());
                            Ref.out.println(sb);
                        }
                        default -> {}
                    }
                } catch (Throwable e) {
                    Ref.out.println("error " + f[1] + " " + e);
                }
            }
            if (dig) {
                for (var e : groups.entrySet()) {
                    var crc = new java.util.zip.CRC32();
                    crc.update(String.join("\n", e.getValue()).getBytes(java.nio.charset.StandardCharsets.UTF_8));
                    Ref.out.println("digest " + e.getKey() + " " + String.format("%08x", crc.getValue()) + " " + e.getValue().size());
                }
            }
            return null;
        }).get();
        Ref.exit();
    }

    static LootTable table(String id) {
        return level.getServer().reloadableRegistries().getLootTable(ResourceKey.create(Registries.LOOT_TABLE, Identifier.parse(id)));
    }

    static ItemStack stack(String spec) throws Exception {
        if (spec.equals("-")) return ItemStack.EMPTY;
        var in = items.parse(new StringReader(spec));
        return new ItemStack(in.item(), 1, in.components());
    }

    static String out(List<ItemStack> stacks, RandomSource r) {
        StringBuilder sb = new StringBuilder();
        for (ItemStack s : stacks) {
            if (sb.length() > 0) sb.append(',');
            RegistryFriendlyByteBuf b = Ref.buf();
            ItemStack.OPTIONAL_STREAM_CODEC.encode(b, s);
            sb.append(Ref.hex(b));
        }
        return (sb.length() == 0 ? "-" : sb.toString()) + " " + r.nextLong();
    }

    static String block(String[] f) throws Exception {
        BlockState st = BlockStateParser.parseForBlock(level.holderLookup(Registries.BLOCK), f[3], false).blockState();
        BlockPos pos = new BlockPos(1, 100, 0);
        var b = new LootParams.Builder(level)
            .withParameter(LootContextParams.ORIGIN, Vec3.atCenterOf(pos))
            .withParameter(LootContextParams.BLOCK_STATE, st)
            .withParameter(LootContextParams.TOOL, stack(f[4]));
        if (!f[5].equals("-")) b.withParameter(LootContextParams.EXPLOSION_RADIUS, Float.parseFloat(f[5]));
        RandomSource r = new XoroshiroRandomSource(Long.parseLong(f[6]));
        var res = table(f[2]).getRandomItems(b.create(LootContextParamSets.BLOCK), r);
        return "block " + f[1] + " " + out(res, r);
    }

    @SuppressWarnings("unchecked")
    static String entity(String[] f, Player killer) throws Exception {
        EntityType<?> type = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(f[3]));
        Entity e = type.create(level, EntitySpawnReason.COMMAND);
        e.snapTo(0.5, 100, 0.5, 0, 0);
        for (String fl : f[4].split(",")) {
            switch (fl) {
                case "fire" -> e.setRemainingFireTicks(100);
                case "baby" -> { if (e instanceof net.minecraft.world.entity.AgeableMob a) a.setBaby(true); else if (e instanceof net.minecraft.world.entity.monster.zombie.Zombie z) z.setBaby(true); }
                default -> {}
            }
        }
        killer.setItemSlot(EquipmentSlot.MAINHAND, stack(f[5]));
        String size = e instanceof net.minecraft.world.entity.monster.cubemob.Slime sl ? "" + sl.getSize() : "-";
        String color = e instanceof net.minecraft.world.entity.animal.sheep.Sheep sh ? sh.getColor().getSerializedName() : "-";
        Ref.out.println("ent " + f[1] + " " + e.isOnFire() + " " + (e instanceof LivingEntity le && le.isBaby()) + " " + size + " " + color);
        boolean byPlayer = f[6].equals("1");
        DamageSource ds = byPlayer ? level.damageSources().playerAttack(killer) : level.damageSources().generic();
        if (f[7].equals("lightning")) ds = level.damageSources().lightningBolt();
        var b = new LootParams.Builder(level)
            .withParameter(LootContextParams.THIS_ENTITY, e)
            .withParameter(LootContextParams.ORIGIN, e.position())
            .withParameter(LootContextParams.DAMAGE_SOURCE, ds)
            .withOptionalParameter(LootContextParams.ATTACKING_ENTITY, ds.getEntity())
            .withOptionalParameter(LootContextParams.DIRECT_ATTACKING_ENTITY, ds.getDirectEntity());
        if (byPlayer) b.withParameter(LootContextParams.LAST_DAMAGE_PLAYER, killer);
        RandomSource r = new XoroshiroRandomSource(Long.parseLong(f[8]));
        var res = table(f[2]).getRandomItems(b.create(LootContextParamSets.ENTITY), r);
        e.discard();
        return "entity " + f[1] + " " + out(res, r);
    }
}
