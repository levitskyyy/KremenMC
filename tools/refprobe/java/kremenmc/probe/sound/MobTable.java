package kremenmc.probe.sound;

import java.lang.reflect.Method;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.sounds.SoundEvent;
import net.minecraft.world.entity.*;

// MobTable queries each living entity type's sound accessors on a fresh
// instance (adult, then baby where the type has one):
//
//	mob <type> <adult|baby> <category> <volume> <interval> <ambient> <hurt> <death>
public final class MobTable {
    public static void main(String[] args) throws Exception {
        Env.boot();
        Method vol = LivingEntity.class.getDeclaredMethod("getSoundVolume");
        Method hurt = LivingEntity.class.getDeclaredMethod("getHurtSound", net.minecraft.world.damagesource.DamageSource.class);
        Method death = LivingEntity.class.getDeclaredMethod("getDeathSound");
        Method amb = Mob.class.getDeclaredMethod("getAmbientSound");
        for (Method m : new Method[] {vol, hurt, death, amb}) m.setAccessible(true);
        Env.run(() -> {
            for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) {
                Entity e;
                try {
                    e = t.create(Env.level, EntitySpawnReason.COMMAND);
                } catch (Throwable x) {
                    continue;
                }
                if (!(e instanceof LivingEntity le)) continue;
                String name = BuiltInRegistries.ENTITY_TYPE.getKey(t).getPath();
                for (int baby = 0; baby < 2; baby++) {
                    if (baby == 1) {
                        if (le instanceof AgeableMob am) am.setAge(-24000);
                        else if (le instanceof Mob mm && !le.isBaby()) {
                            mm.setBaby(true);
                            if (!mm.isBaby()) break;
                        } else break;
                    }
                    String a = le instanceof Mob ? str((SoundEvent) amb.invoke(le)) : "-";
                    String interval = le instanceof Mob mm ? "" + mm.getAmbientSoundInterval() : "-";
                    Env.out.println("mob " + name + " " + (baby == 1 ? "baby" : "adult") + " " + le.getSoundSource().getName() + " " + vol.invoke(le) + " " + interval + " " + a + " "
                        + str((SoundEvent) hurt.invoke(le, Env.level.damageSources().generic())) + " " + str((SoundEvent) death.invoke(le)));
                }
            }
        });
        Env.exit();
    }

    static String str(SoundEvent s) {
        return s == null ? "-" : s.location().toString();
    }
}
