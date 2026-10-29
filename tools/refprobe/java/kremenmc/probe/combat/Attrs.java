package kremenmc.probe.combat;

import java.lang.reflect.Method;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.effect.MobEffect;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.ai.attributes.Attribute;
import net.minecraft.world.entity.ai.attributes.AttributeInstance;
import net.minecraft.world.entity.ai.attributes.DefaultAttributes;
import net.minecraft.world.entity.ai.attributes.RangedAttribute;

// Attrs prints the attribute registry, default attribute bases per living
// entity type (supplier and fresh instance), per-type gravity and air drag,
// and mob effect attribute modifiers for amplifiers 0..2.
public final class Attrs {
    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            for (Attribute a : BuiltInRegistries.ATTRIBUTE) {
                var h = BuiltInRegistries.ATTRIBUTE.wrapAsHolder(a);
                String k = h.unwrapKey().get().identifier().toString();
                double mn = Double.NaN, mx = Double.NaN;
                if (a instanceof RangedAttribute r) { mn = r.getMinValue(); mx = r.getMaxValue(); }
                o.println("attr " + BuiltInRegistries.ATTRIBUTE.getId(a) + " " + k + " " + a.getDefaultValue() + " " + mn + " " + mx + " " + a.isClientSyncable());
            }
            Method grav = Entity.class.getDeclaredMethod("getDefaultGravity");
            grav.setAccessible(true);
            Method drag = Entity.class.getDeclaredMethod("getAirDrag");
            drag.setAccessible(true);
            for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) {
                String key = EntityType.getKey(t).toString();
                Entity e = null;
                try { e = t.create(Env.level, EntitySpawnReason.COMMAND); } catch (Throwable ex) {}
                if (e != null) {
                    o.println("phys " + key + " gravity=" + grav.invoke(e) + " drag=" + drag.invoke(e) + " w=" + t.getWidth() + " h=" + t.getHeight() + " eye=" + e.getEyeHeight() + " class=" + e.getClass().getSimpleName());
                }
                if (DefaultAttributes.hasSupplier(t)) {
                    @SuppressWarnings("unchecked")
                    var sup = DefaultAttributes.getSupplier((EntityType<? extends LivingEntity>) t);
                    StringBuilder sb = new StringBuilder("base " + key);
                    for (Attribute a : BuiltInRegistries.ATTRIBUTE) {
                        var h = BuiltInRegistries.ATTRIBUTE.wrapAsHolder(a);
                        if (!sup.hasAttribute(h)) continue;
                        sb.append(' ').append(h.unwrapKey().get().identifier().getPath());
                        if (sup.getBaseValue(h) != a.getDefaultValue()) sb.append('=').append(sup.getBaseValue(h));
                    }
                    o.println(sb);
                    if (e instanceof LivingEntity le) {
                        StringBuilder ib = new StringBuilder("inst " + key);
                        for (Attribute a : BuiltInRegistries.ATTRIBUTE) {
                            var h = BuiltInRegistries.ATTRIBUTE.wrapAsHolder(a);
                            AttributeInstance ai = le.getAttribute(h);
                            if (ai == null) continue;
                            if (!sup.hasAttribute(h) || ai.getBaseValue() != sup.getBaseValue(h) || !ai.getModifiers().isEmpty())
                                ib.append(' ').append(h.unwrapKey().get().identifier().getPath()).append('=').append(ai.getBaseValue()).append('/').append(ai.getValue());
                        }
                        ib.append(" health=").append(le.getHealth());
                        o.println(ib);
                    }
                }
                if (e != null) e.discard();
            }
            for (MobEffect m : BuiltInRegistries.MOB_EFFECT) {
                String key = BuiltInRegistries.MOB_EFFECT.getKey(m).toString();
                for (int amp = 0; amp <= 2; amp++) {
                    final int a0 = amp;
                    m.createModifiers(amp, (attr, mod) -> o.println("eff " + key + " " + a0 + " " + attr.unwrapKey().get().identifier().getPath() + " " + mod.id() + " " + mod.amount() + " " + mod.operation().getSerializedName()));
                }
            }
        });
        Env.exit();
    }
}
