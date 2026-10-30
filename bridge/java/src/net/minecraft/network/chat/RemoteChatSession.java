package net.minecraft.network.chat;

import java.util.UUID;

/** A player's chat signing session. KremenMC does not relay signed chat sessions to clients. */
public record RemoteChatSession(UUID sessionId) {
    public Data asData() { return new Data(sessionId); }

    public record Data(UUID sessionId) {}
}
