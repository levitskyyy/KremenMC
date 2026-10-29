package kremenmc.probe;

import net.minecraft.SharedConstants;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;

public final class Smoke {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        System.out.println("version " + SharedConstants.getCurrentVersion().name() + " protocol " + SharedConstants.getProtocolVersion());
        RandomSource r = new XoroshiroRandomSource(0L);
        System.out.println("xoroshiro(0) " + r.nextLong() + " " + r.nextInt(100) + " " + r.nextDouble());
    }
}
