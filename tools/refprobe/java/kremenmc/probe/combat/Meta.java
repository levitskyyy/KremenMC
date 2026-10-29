package kremenmc.probe.combat;

import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import net.minecraft.network.syncher.EntityDataAccessor;

// Meta prints the synched entity data accessor ids of the classes combat
// writes (living health, player absorption, arrows).
public final class Meta {
    public static void main(String[] args) throws Exception {
        Env.init();
        var o = Env.out;
        String[] classes = {"net.minecraft.world.entity.Entity", "net.minecraft.world.entity.LivingEntity", "net.minecraft.world.entity.Mob", "net.minecraft.world.entity.AgeableMob", "net.minecraft.world.entity.Avatar", "net.minecraft.world.entity.player.Player",
            "net.minecraft.world.entity.projectile.arrow.AbstractArrow", "net.minecraft.world.entity.projectile.AbstractArrow", "net.minecraft.world.entity.projectile.arrow.ThrownTrident", "net.minecraft.world.entity.projectile.ThrownTrident",
            "net.minecraft.world.entity.projectile.ThrowableItemProjectile", "net.minecraft.world.entity.projectile.throwableitemprojectile.ThrowableItemProjectile", "net.minecraft.world.entity.projectile.arrow.Arrow", "net.minecraft.world.entity.projectile.Arrow"};
        for (String cn : classes) {
            Class<?> c;
            try { c = Class.forName(cn); } catch (ClassNotFoundException e) { continue; }
            for (Field f : c.getDeclaredFields()) {
                if (!Modifier.isStatic(f.getModifiers()) || f.getType() != EntityDataAccessor.class) continue;
                f.setAccessible(true);
                EntityDataAccessor<?> a = (EntityDataAccessor<?>) f.get(null);
                o.println("meta " + c.getSimpleName() + " " + f.getName() + " " + a.id() + " " + net.minecraft.network.syncher.EntityDataSerializers.getSerializedId(a.serializer()));
            }
        }
        o.flush();
        Runtime.getRuntime().halt(0);
    }
}
