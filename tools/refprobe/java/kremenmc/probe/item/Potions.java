package kremenmc.probe.item;

import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;

/** Prints every potion's effects: potion effect duration amplifier. */
public final class Potions {
    public static void main(String[] args) {
        Ref.quietLogsUnchecked();
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (var p : BuiltInRegistries.POTION) {
            String name = BuiltInRegistries.POTION.getKey(p).toString();
            if (p.getEffects().isEmpty()) Ref.out.println("potion " + name + " -");
            for (var e : p.getEffects()) {
                Ref.out.println("potion " + name + " " + e.getEffect().unwrapKey().get().identifier() + " " + e.getDuration() + " " + e.getAmplifier());
            }
        }
        Ref.exit();
    }
}
