package org.bukkit.craftbukkit.inventory;

import net.minecraft.world.item.ItemStack;

/**
 * Conversions between Bukkit and NMS item stacks, looked up reflectively by
 * plugins. Item data lives on the Go side, so the NMS copy only carries the
 * amount and nothing converts back.
 */
public final class CraftItemStack {
    private CraftItemStack() {}

    public static ItemStack asNMSCopy(org.bukkit.inventory.ItemStack stack) {
        return stack == null ? ItemStack.EMPTY : new ItemStack(null, stack.getAmount());
    }

    public static org.bukkit.inventory.ItemStack asBukkitCopy(ItemStack stack) { return null; }
}
