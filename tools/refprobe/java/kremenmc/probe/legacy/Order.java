package kremenmc.probe.legacy;

import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;

// Order prints the structure registry in its runtime iteration order with
// each structure's decoration step.
public final class Order {
    public static void main(String[] args) throws Exception {
        var out = new java.io.PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var reg = VanillaRegistries.createWorldLookup().lookupOrThrow(Registries.STRUCTURE);
        int i = 0;
        for (var h : (Iterable<net.minecraft.core.Holder.Reference<net.minecraft.world.level.levelgen.structure.Structure>>) reg.listElements()::iterator) {
            out.println((i++) + " " + h.key().identifier() + " " + h.value().step());
        }
    }
}
