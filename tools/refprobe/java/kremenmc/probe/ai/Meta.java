package kremenmc.probe.ai;

import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.syncher.EntityDataSerializers;
import net.minecraft.resources.Identifier;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.level.block.Blocks;

// Meta prints the synched data that AI state changes set (creeper swell,
// enderman carried block, wolf sitting, aggressive flag) as
// "meta <case> <index> <serializer> <value>".
public final class Meta {
    public static void main(String[] args) throws Exception {
        Env.boot();
        Env.run(() -> {
            dump("plain_creeper", mob("creeper"));
            var c = (net.minecraft.world.entity.monster.Creeper) mob("creeper");
            c.setSwellDir(1);
            dump("creeper_swell", c);
            var e = (net.minecraft.world.entity.monster.Enderman) mob("enderman");
            e.setCarriedBlock(Blocks.GRASS_BLOCK.defaultBlockState());
            dump("enderman_carry", e);
            var w = (net.minecraft.world.entity.TamableAnimal) mob("wolf");
            w.setInSittingPose(true);
            dump("wolf_sit", w);
            var s = mob("skeleton");
            s.setAggressive(true);
            dump("skeleton_aggressive", s);
        });
        Env.exit();
    }

    static Mob mob(String t) {
        return (Mob) BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse("minecraft:" + t)).create(Env.level, EntitySpawnReason.COMMAND);
    }

    static void dump(String name, Mob m) {
        var vals = m.getEntityData().getNonDefaultValues();
        if (vals == null) return;
        for (var v : vals)
            Env.out.println("meta " + name + " " + v.id() + " " + EntityDataSerializers.getSerializedId(v.serializer()) + " " + v.value());
    }
}
