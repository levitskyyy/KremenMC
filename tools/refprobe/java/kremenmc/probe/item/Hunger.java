package kremenmc.probe.item;

import java.nio.file.*;
import java.util.*;
import net.minecraft.world.Difficulty;
import net.minecraft.world.food.FoodProperties;
import net.minecraft.world.level.GameType;

/**
 * FoodData oracle. "tick" cases run FoodData.tick on a survival player for
 * n ticks, adding exhaustion each tick, and print health/food/saturation per
 * tick; "eat" cases print the state after eating.
 */
public final class Hunger {
    // The fake player is not ticked, so hurt invulnerability would never
    // wear off; reset it as a real tick would between starvation hits.
    static final java.lang.reflect.Field INVULNERABLE;
    static {
        try {
            INVULNERABLE = net.minecraft.world.entity.Entity.class.getDeclaredField("invulnerableTime");
            INVULNERABLE.setAccessible(true);
        } catch (ReflectiveOperationException e) {
            throw new RuntimeException(e);
        }
    }

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var level = server.overworld();
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        server.submit(() -> {
            for (String line : lines) {
                String[] f = line.strip().split(" ");
                if (f.length < 2) continue;
                try {
                    FakePlayer p = new FakePlayer(level);
                    p.setGameMode(GameType.SURVIVAL);
                    var fd = p.getFoodData();
                    if (f[0].equals("tick")) {
                        server.setDifficulty(Difficulty.byId(Integer.parseInt(f[2])), true);
                        p.setHealth(Float.parseFloat(f[3]));
                        fd.setFoodLevel(Integer.parseInt(f[4]));
                        fd.setSaturation(Float.parseFloat(f[5]));
                        float ex = Float.parseFloat(f[6]);
                        int n = Integer.parseInt(f[7]);
                        StringBuilder sb = new StringBuilder("tick ").append(f[1]);
                        for (int i = 0; i < n; i++) {
                            fd.addExhaustion(ex);
                            fd.tick(p);
                            INVULNERABLE.setInt(p, 0);
                            sb.append(' ').append(p.getHealth()).append('/').append(fd.getFoodLevel()).append('/').append(fd.getSaturationLevel());
                        }
                        Ref.out.println(sb);
                    } else if (f[0].equals("eat")) {
                        fd.setFoodLevel(Integer.parseInt(f[2]));
                        fd.setSaturation(Float.parseFloat(f[3]));
                        fd.eat(new FoodProperties(Integer.parseInt(f[4]), Float.parseFloat(f[5]), false));
                        Ref.out.println("eat " + f[1] + " " + fd.getFoodLevel() + "/" + fd.getSaturationLevel());
                    }
                } catch (Exception e) {
                    Ref.out.println("error " + f[1] + " " + e);
                }
            }
            return null;
        }).get();
        Ref.exit();
    }
}
