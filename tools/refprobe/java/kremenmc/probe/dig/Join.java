package kremenmc.probe.dig;

import java.util.*;
import com.mojang.authlib.GameProfile;
import io.netty.channel.embedded.EmbeddedChannel;
import net.minecraft.network.Connection;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.PacketFlow;
import net.minecraft.network.protocol.game.*;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.network.CommonListenerCookie;
import net.minecraft.world.level.GameType;

/** Prints the play packets vanilla sends a joining survival player, with the fields that steer client-side mining. */
public final class Join {
    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        server.submit(() -> {

            var prof = new GameProfile(UUID.randomUUID(), "probe");
            Connection conn = new Connection(PacketFlow.SERVERBOUND);
            EmbeddedChannel ch = new EmbeddedChannel(conn);
            ServerPlayer p = new ServerPlayer(server, server.overworld(), prof, ClientInformation.createDefault());
            server.getPlayerList().placeNewPlayer(conn, p, CommonListenerCookie.createInitial(prof, false));
            Ref.out.println("gm=" + p.gameMode.getGameModeForPlayer());
            for (int t = 0; t < 3; t++) {
                p.doTick();
                server.getPlayerList().tick();
            }
            p.connection.tick();
            Object o;
            while ((o = ch.readOutbound()) != null) {
                if (!(o instanceof Packet<?> pk)) continue;
                String s = pk.getClass().getSimpleName();
                if (pk instanceof ClientboundLoginPacket l) s += " " + l.commonPlayerSpawnInfo();
                else if (pk instanceof ClientboundPlayerAbilitiesPacket a) s += " inv=" + a.isInvulnerable() + " fly=" + a.isFlying() + " canFly=" + a.canFly() + " insta=" + a.canInstabuild() + " fs=" + a.getFlyingSpeed() + " ws=" + a.getWalkingSpeed();
                else if (pk instanceof ClientboundGameEventPacket g) s += " " + g.getEvent() + " " + g.getParam();
                else if (pk instanceof ClientboundUpdateAttributesPacket u) {
                    s += " e=" + u.getEntityId();
                    for (var a : u.getValues()) s += " " + a.attribute().getRegisteredName() + "=" + a.base() + a.modifiers();
                } else if (pk instanceof ClientboundSetEntityDataPacket d) s += " e=" + d.id() + " n=" + d.packedItems().size();
                Ref.out.println(s);
            }
            return null;
        }).get();
        Ref.exit();
    }
}
