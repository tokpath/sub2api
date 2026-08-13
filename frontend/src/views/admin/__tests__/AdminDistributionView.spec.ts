import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.hoisted(() => {
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    },
    configurable: true,
  })
})

import AdminDistributionView from '../distribution/AdminDistributionView.vue'

const {
  createDistributionMember,
  createDistributionOrganization,
  createDistributionPromotionLink,
  getDistributionReconciliationSummary,
  getDistributionStats,
  listDistributionAttributions,
  listDistributionAttributionAudits,
  listDistributionAlertEvents,
  listDistributionCommissions,
  listDistributionMembers,
  listDistributionOrganizations,
  listDistributionPromotionLinks,
  listDistributionReconciliationIssueActions,
  listDistributionReconciliationIssues,
  listDistributionWalletRequests,
  listDistributionWalletTransactions,
  listDistributionWallets,
  rechargeDistributionWallet,
  reviewDistributionWalletRequest,
  refundDistributionWallet,
  reverseDistributionCommission,
  resolveDistributionReconciliationIssue,
  runDistributionReconciliation,
  settleDistributionCommission,
  updateDistributionAttribution,
  updateDistributionOrganization,
  updateDistributionWalletWarningThreshold,
} = vi.hoisted(() => ({
  createDistributionMember: vi.fn(),
  createDistributionOrganization: vi.fn(),
  createDistributionPromotionLink: vi.fn(),
  getDistributionReconciliationSummary: vi.fn(),
  getDistributionStats: vi.fn(),
  listDistributionAttributions: vi.fn(),
  listDistributionAttributionAudits: vi.fn(),
  listDistributionAlertEvents: vi.fn(),
  listDistributionCommissions: vi.fn(),
  listDistributionMembers: vi.fn(),
  listDistributionOrganizations: vi.fn(),
  listDistributionPromotionLinks: vi.fn(),
  listDistributionReconciliationIssueActions: vi.fn(),
  listDistributionReconciliationIssues: vi.fn(),
  listDistributionWalletRequests: vi.fn(),
  listDistributionWalletTransactions: vi.fn(),
  listDistributionWallets: vi.fn(),
  rechargeDistributionWallet: vi.fn(),
  reviewDistributionWalletRequest: vi.fn(),
  refundDistributionWallet: vi.fn(),
  reverseDistributionCommission: vi.fn(),
  resolveDistributionReconciliationIssue: vi.fn(),
  runDistributionReconciliation: vi.fn(),
  settleDistributionCommission: vi.fn(),
  updateDistributionAttribution: vi.fn(),
  updateDistributionOrganization: vi.fn(),
  updateDistributionWalletWarningThreshold: vi.fn(),
}))

const showError = vi.fn()
const showSuccess = vi.fn()
const { listUsers, getUserById } = vi.hoisted(() => ({
  listUsers: vi.fn(),
  getUserById: vi.fn(),
}))

vi.mock('@/api/admin/distribution', () => ({
  createDistributionMember,
  createDistributionOrganization,
  createDistributionPromotionLink,
  getDistributionReconciliationSummary,
  getDistributionStats,
  listDistributionAttributions,
  listDistributionAttributionAudits,
  listDistributionAlertEvents,
  listDistributionCommissions,
  listDistributionMembers,
  listDistributionOrganizations,
  listDistributionPromotionLinks,
  listDistributionReconciliationIssueActions,
  listDistributionReconciliationIssues,
  listDistributionWalletRequests,
  listDistributionWalletTransactions,
  listDistributionWallets,
  rechargeDistributionWallet,
  reviewDistributionWalletRequest,
  refundDistributionWallet,
  reverseDistributionCommission,
  resolveDistributionReconciliationIssue,
  runDistributionReconciliation,
  settleDistributionCommission,
  updateDistributionAttribution,
  updateDistributionOrganization,
  updateDistributionWalletWarningThreshold,
  adminDistributionAPI: {
    createDistributionMember,
    createDistributionOrganization,
    createDistributionPromotionLink,
    getDistributionReconciliationSummary,
    getDistributionStats,
    listDistributionAttributions,
    listDistributionAttributionAudits,
    listDistributionAlertEvents,
    listDistributionCommissions,
    listDistributionMembers,
    listDistributionOrganizations,
    listDistributionPromotionLinks,
    listDistributionReconciliationIssueActions,
    listDistributionReconciliationIssues,
    listDistributionWalletRequests,
    listDistributionWalletTransactions,
    listDistributionWallets,
    rechargeDistributionWallet,
    reviewDistributionWalletRequest,
    refundDistributionWallet,
    reverseDistributionCommission,
    resolveDistributionReconciliationIssue,
    runDistributionReconciliation,
    settleDistributionCommission,
    updateDistributionAttribution,
    updateDistributionOrganization,
    updateDistributionWalletWarningThreshold,
  },
  default: {
    createDistributionMember,
    createDistributionOrganization,
    createDistributionPromotionLink,
    getDistributionReconciliationSummary,
    getDistributionStats,
    listDistributionAttributions,
    listDistributionAttributionAudits,
    listDistributionCommissions,
    listDistributionMembers,
    listDistributionOrganizations,
    listDistributionPromotionLinks,
    listDistributionReconciliationIssueActions,
    listDistributionReconciliationIssues,
    listDistributionWalletRequests,
    listDistributionWalletTransactions,
    listDistributionWallets,
    rechargeDistributionWallet,
    reviewDistributionWalletRequest,
    refundDistributionWallet,
    reverseDistributionCommission,
    resolveDistributionReconciliationIssue,
    runDistributionReconciliation,
    settleDistributionCommission,
    updateDistributionAttribution,
    updateDistributionOrganization,
    updateDistributionWalletWarningThreshold,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/api/admin', () => ({
  usersAPI: {
    list: listUsers,
    getById: getUserById,
  },
}))

vi.mock('@/utils/apiError', () => ({
  extractI18nErrorMessage: () => 'error',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const mockRoute = vi.hoisted(() => ({
  path: '/admin/distribution/organizations',
  params: { tab: 'organizations' },
}))

vi.mock('vue-router', () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({
    replace: vi.fn(),
  }),
}))

const DataTableStub = {
  props: ['columns', 'data', 'loading'],
  template: `
    <div>
      <div v-for="row in data" :key="row.channel_org_id || row.id" class="wallet-row">
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `,
}

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show"><slot /></div>',
}

describe('AdminDistributionView', () => {
  beforeEach(() => {
    mockRoute.path = '/admin/distribution/organizations'
    mockRoute.params = { tab: 'organizations' }

    showError.mockReset()
    showSuccess.mockReset()
    listUsers.mockReset()
    getUserById.mockReset()
    createDistributionMember.mockReset()
    createDistributionOrganization.mockReset()
    createDistributionPromotionLink.mockReset()
    getDistributionReconciliationSummary.mockReset()
    getDistributionStats.mockReset()
    listDistributionAttributions.mockReset()
    listDistributionAttributionAudits.mockReset()
    listDistributionAlertEvents.mockReset()
    listDistributionCommissions.mockReset()
    listDistributionMembers.mockReset()
    listDistributionOrganizations.mockReset()
    listDistributionPromotionLinks.mockReset()
    listDistributionReconciliationIssueActions.mockReset()
    listDistributionReconciliationIssues.mockReset()
    listDistributionWalletRequests.mockReset()
    listDistributionWalletTransactions.mockReset()
    listDistributionWallets.mockReset()
    rechargeDistributionWallet.mockReset()
    reviewDistributionWalletRequest.mockReset()
    refundDistributionWallet.mockReset()
    reverseDistributionCommission.mockReset()
    resolveDistributionReconciliationIssue.mockReset()
    runDistributionReconciliation.mockReset()
    settleDistributionCommission.mockReset()
    updateDistributionAttribution.mockReset()
    updateDistributionOrganization.mockReset()
    updateDistributionWalletWarningThreshold.mockReset()

    getDistributionStats.mockResolvedValue({
      organization_count: 1,
      platform_count: 0,
      reseller_count: 1,
      oem_count: 0,
      member_count: 0,
      agent_count: 0,
      kol1_count: 0,
      kol2_count: 0,
      promotion_link_count: 0,
      attribution_count: 0,
      commission_count: 0,
      wallet_count: 1,
      prepaid_balance_total: 120,
      commission_reserved_total: 0,
      total_recharged: 120,
      total_consumed: 0,
      frozen_commission_amount: 0,
      available_commission_amount: 0,
      settled_commission_amount: 0,
    })
    getDistributionReconciliationSummary.mockResolvedValue({
      latest_run: null,
      open_count: 0,
      critical_count: 0,
      warning_count: 0,
      ignored_count: 0,
      resolved_count: 0,
      repairable_count: 0,
    })
    listDistributionReconciliationIssueActions.mockResolvedValue([])
    listDistributionReconciliationIssues.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    runDistributionReconciliation.mockResolvedValue({
      id: 1,
      trigger_type: 'manual',
      status: 'completed',
      issue_count: 0,
      resolved_count: 0,
      error_message: '',
      started_at: '2026-05-24T00:00:00Z',
      completed_at: '2026-05-24T00:00:01Z',
    })
    listDistributionOrganizations.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionWallets.mockResolvedValue({
      items: [
        {
          channel_org_id: 88,
          organization_name: 'Independent Agent',
          organization_type: 'reseller',
          prepaid_balance: 120,
          commission_reserved: 0,
          total_recharged: 200,
          total_consumed: 80,
          warning_threshold: 50,
          status: 'active',
          created_at: '2026-05-24T00:00:00Z',
          updated_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionAlertEvents.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionAttributions.mockResolvedValue({
      items: [
        {
          user_id: 7,
          user_email: 'user@example.com',
          username: 'example-user',
          channel_org_id: 88,
          referrer_member_id: 66,
          promotion_link_id: 33,
          bound_at: '2026-05-24T00:00:00Z',
          bound_source: 'link',
          bound_by: 'system',
          created_at: '2026-05-24T00:00:00Z',
          updated_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionAttributionAudits.mockResolvedValue({
      items: [
        {
          id: 1,
          user_id: 7,
          user_email: 'user@example.com',
          username: 'example-user',
          previous_channel_org_id: 66,
          previous_referrer_member_id: 67,
          previous_promotion_link_id: 68,
          previous_bound_source: 'registration',
          previous_bound_by: 'system',
          new_channel_org_id: 88,
          new_referrer_member_id: 77,
          new_promotion_link_id: 44,
          new_bound_source: 'manual',
          new_bound_by: 'admin',
          note: 'manual attribution fix',
          operator_user_id: 9,
          operator_user_email: 'admin@example.com',
          operator_username: 'admin-user',
          created_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionWalletTransactions.mockResolvedValue({
      items: [
        {
          id: 9001,
          channel_org_id: 88,
          organization_name: 'Independent Agent',
          organization_type: 'reseller',
          transaction_type: 'refund',
          amount: 80,
          prepaid_balance_before: 120,
          prepaid_balance_after: 40,
          commission_reserved_before: 0,
          commission_reserved_after: 0,
          reference_no: 'RF-1',
          note: 'manual refund',
          operator_user_id: 9,
          created_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    listDistributionWalletRequests.mockResolvedValue({
      items: [
        {
          id: 31,
          channel_org_id: 88,
          organization_name: 'Independent Agent',
          organization_type: 'reseller',
          request_type: 'recharge',
          amount: 120,
          reference_no: 'BANK-1',
          note: 'bank transfer',
          status: 'pending',
          created_by_user_id: 7,
          created_by_user_email: 'manager@example.com',
          created_by_username: 'manager',
          reviewed_by_user_id: null,
          reviewed_by_user_email: '',
          reviewed_by_username: '',
          review_note: '',
          reviewed_at: null,
          created_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    refundDistributionWallet.mockResolvedValue({
      wallet: {
        channel_org_id: 88,
        organization_name: 'Independent Agent',
        organization_type: 'reseller',
        prepaid_balance: 40,
        commission_reserved: 0,
        total_recharged: 200,
        total_consumed: 80,
        warning_threshold: 50,
        status: 'active',
        created_at: '2026-05-24T00:00:00Z',
        updated_at: '2026-05-24T00:00:00Z',
      },
      refund_amount: 80,
      fee_rate: 0.1,
      fee_amount: 8,
      net_amount: 72,
      reference_no: 'RF-1',
      note: 'manual refund',
      processed_mock: true,
    })
    reviewDistributionWalletRequest.mockResolvedValue({
      id: 31,
      channel_org_id: 88,
      organization_name: 'Independent Agent',
      organization_type: 'reseller',
      request_type: 'recharge',
      amount: 120,
      reference_no: 'BANK-1',
      note: 'bank transfer',
      status: 'approved',
      created_by_user_id: 7,
      created_by_user_email: 'manager@example.com',
      created_by_username: 'manager',
      reviewed_by_user_id: 9,
      reviewed_by_user_email: 'admin@example.com',
      reviewed_by_username: 'admin',
      review_note: '到账确认',
      reviewed_at: '2026-05-24T01:00:00Z',
      created_at: '2026-05-24T00:00:00Z',
    })
    updateDistributionAttribution.mockResolvedValue({
      user_id: 7,
      channel_org_id: 99,
      referrer_member_id: 77,
      promotion_link_id: 44,
      bound_at: '2026-05-24T00:00:00Z',
      bound_source: 'manual',
      bound_by: 'admin',
      audit_id: 1,
      created_at: '2026-05-24T00:00:00Z',
      updated_at: '2026-05-24T00:00:00Z',
    })
    listUsers.mockResolvedValue({
      items: [
        {
          id: 42,
          username: 'alice',
          email: 'alice@example.com',
        },
      ],
      total: 1,
      page: 1,
      page_size: 10,
      pages: 1,
    })
    getUserById.mockResolvedValue({
      id: 42,
      username: 'alice',
      email: 'alice@example.com',
    })
  })

  it('filters members by selecting a searched user', async () => {
    vi.useFakeTimers()
    mockRoute.path = '/admin/distribution/members'
    mockRoute.params = { tab: 'members' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const filterSearch = wrapper.find('input[placeholder="admin.usage.searchUserPlaceholder"]')
    await filterSearch.trigger('focus')
    await filterSearch.setValue('alice')
    vi.advanceTimersByTime(300)
    await flushPromises()

    const option = wrapper.findAll('button').find((button) => button.text().includes('alice@example.com'))
    expect(option).toBeDefined()
    await option!.trigger('click')
    await flushPromises()

    expect(listDistributionMembers).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      channel_org_id: undefined,
      user_id: 42,
      role_type: undefined,
      alert_type: undefined,
      severity: undefined,
      request_type: undefined,
      status: undefined,
      transaction_type: undefined,
    })
    vi.useRealTimers()
  })

  it('submits member creation with the selected user instead of a raw id input', async () => {
    vi.useFakeTimers()
    mockRoute.path = '/admin/distribution/members'
    mockRoute.params = { tab: 'members' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.createMember')
    expect(createButton).toBeDefined()
    await createButton!.trigger('click')
    await flushPromises()

    const forms = wrapper.findAll('form')
    const memberForm = forms[0]
    const view = wrapper.vm as unknown as {
      memberForm: { channel_org_id: number; level_code: string; commission_rate: number }
    }
    view.memberForm.channel_org_id = 88
    view.memberForm.level_code = 'L1'
    view.memberForm.commission_rate = 0.2
    await wrapper.vm.$nextTick()

    const memberUserSearch = memberForm.find('input[placeholder="admin.usage.searchUserPlaceholder"]')
    await memberUserSearch.trigger('focus')
    await memberUserSearch.setValue('alice')
    vi.advanceTimersByTime(300)
    await flushPromises()

    const option = wrapper.findAll('button').find((button) => button.text().includes('alice@example.com'))
    expect(option).toBeDefined()
    await option!.trigger('click')
    await flushPromises()

    await memberForm.trigger('submit.prevent')
    await flushPromises()

    expect(createDistributionMember).toHaveBeenCalledWith({
      channel_org_id: 88,
      user_id: 42,
      role_type: 'agent',
      parent_member_id: undefined,
      level_code: 'L1',
      commission_rate: 0.2,
      status: 'active',
    })
    vi.useRealTimers()
  })

  it('opens wallet refund dialog and submits mock refund request', async () => {
    mockRoute.path = '/admin/distribution/wallets'
    mockRoute.params = { tab: 'wallets' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const refundButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.refundWallet')
    expect(refundButton).toBeDefined()

    await refundButton!.trigger('click')
    await flushPromises()

    const forms = wrapper.findAll('form')
    const refundForm = forms[forms.length - 1]
    const amountInput = refundForm.find('input[type="number"][min="0.0001"]')
    const referenceInput = refundForm.find('input:not([type="number"])')
    const noteInput = refundForm.find('textarea')

    await amountInput.setValue('80')
    await referenceInput.setValue('RF-1')
    await noteInput.setValue('manual refund')

    await refundForm.trigger('submit.prevent')
    await flushPromises()

    expect(refundDistributionWallet).toHaveBeenCalledWith(88, {
      amount: 80,
      reference_no: 'RF-1',
      note: 'manual refund',
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.distribution.messages.walletRefunded')
  })

  it('loads wallet transactions from the route segment and does not render top tabs', async () => {
    mockRoute.path = '/admin/distribution/wallet-transactions'
    mockRoute.params = { tab: 'wallet-transactions' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(listDistributionWalletTransactions).toHaveBeenCalledWith({
      page: 1,
      page_size: 20,
      channel_org_id: undefined,
      role_type: undefined,
      transaction_type: undefined,
    })
    expect(wrapper.text()).not.toContain('admin.distribution.tabs.walletTransactions')
    expect(wrapper.text()).not.toContain('admin.distribution.stats.organizations')
    expect(wrapper.text()).not.toContain('admin.distribution.stats.commissionUpperRatio')
  })

  it('only exposes the settlement action for available commissions', async () => {
    mockRoute.path = '/admin/distribution/commissions'
    mockRoute.params = { tab: 'commissions' }
    listDistributionCommissions.mockResolvedValueOnce({
      items: [
        { id: 1001, status: 'frozen' },
        { id: 1002, status: 'available' },
        { id: 1003, status: 'settled' },
      ],
      total: 3,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const settleButtons = wrapper.findAll('button').filter((button) => button.text() === 'admin.distribution.actions.settleCommission')
    expect(settleButtons).toHaveLength(1)
  })

  it('reviews a pending wallet request from the wallet requests tab', async () => {
    mockRoute.path = '/admin/distribution/wallet-requests'
    mockRoute.params = { tab: 'wallet-requests' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const approveButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.approveWalletRequest')
    expect(approveButton).toBeDefined()

    await approveButton!.trigger('click')
    await flushPromises()

    expect(reviewDistributionWalletRequest).toHaveBeenCalledWith(31, {
      action: 'approve',
      review_note: '',
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.distribution.messages.walletRequestReviewed')
  })

  it('opens attribution dialog and submits manual attribution adjustment', async () => {
    vi.useFakeTimers()
    mockRoute.path = '/admin/distribution/attributions'
    mockRoute.params = { tab: 'attributions' }
    listDistributionMembers.mockResolvedValueOnce({
      items: [
        {
          member_id: 77,
          user_id: 12,
          user_email: 'ref@example.com',
          username: 'referrer',
          channel_org_id: 99,
          role_type: 'agent',
          parent_member_id: null,
          level_code: '',
          commission_rate: 0.2,
          status: 'active',
          created_at: '2026-05-24T00:00:00Z',
          updated_at: '2026-05-24T00:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const adjustButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.adjustAttribution')
    expect(adjustButton).toBeDefined()

    await adjustButton!.trigger('click')
    await flushPromises()

    const view = wrapper.vm as unknown as {
      attributionForm: {
        channel_org_id: number
        referrer_member_id: number | null
        promotion_link_id: number | null
        note: string
      }
    }
    view.attributionForm.channel_org_id = 99
    view.attributionForm.referrer_member_id = 77
    view.attributionForm.promotion_link_id = 44
    view.attributionForm.note = 'manual attribution fix'
    await wrapper.vm.$nextTick()

    const forms = wrapper.findAll('form')
    const attributionForm = forms[forms.length - 1]
    await attributionForm.trigger('submit.prevent')
    await flushPromises()

    expect(updateDistributionAttribution).toHaveBeenCalledWith(7, {
      channel_org_id: 99,
      referrer_member_id: 77,
      promotion_link_id: 44,
      note: 'manual attribution fix',
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.distribution.messages.attributionUpdated')
    vi.useRealTimers()
  })

  it('loads attribution audits for the selected user', async () => {
    mockRoute.path = '/admin/distribution/attributions'
    mockRoute.params = { tab: 'attributions' }

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const auditButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.viewAttributionAudits')
    expect(auditButton).toBeDefined()

    await auditButton!.trigger('click')
    await flushPromises()

    expect(listDistributionAttributionAudits).toHaveBeenCalledWith({
      user_id: 7,
      page: 1,
      page_size: 20,
    })
  })

  it('loads distribution alert events from the route segment', async () => {
    mockRoute.path = '/admin/distribution/alert-events'
    mockRoute.params = { tab: 'alert-events' }

    mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(listDistributionAlertEvents).toHaveBeenCalledWith({
      page: 1,
      page_size: 20,
      channel_org_id: undefined,
      alert_type: undefined,
      severity: undefined,
      status: undefined,
      request_type: undefined,
      role_type: undefined,
      transaction_type: undefined,
    })
  })

  it('runs financial reconciliation from the reconciliation route and refreshes the result', async () => {
    mockRoute.path = '/admin/distribution/reconciliation'
    mockRoute.params = { tab: 'reconciliation' }
    getDistributionReconciliationSummary.mockResolvedValue({
      latest_run: null,
      open_count: 2,
      critical_count: 1,
      warning_count: 1,
      ignored_count: 0,
      resolved_count: 3,
      repairable_count: 1,
    })
    runDistributionReconciliation.mockResolvedValue({
      id: 42,
      trigger_type: 'manual',
      status: 'completed',
      issue_count: 2,
      resolved_count: 1,
      error_message: '',
      started_at: '2026-05-24T00:00:00Z',
      completed_at: '2026-05-24T00:00:01Z',
    })

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(listDistributionReconciliationIssues).toHaveBeenCalledTimes(1)
    expect(getDistributionReconciliationSummary).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('admin.distribution.reconciliation.openIssues')

    const runButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.runReconciliation')
    expect(runButton).toBeDefined()
    await runButton!.trigger('click')
    await flushPromises()

    expect(runDistributionReconciliation).toHaveBeenCalledTimes(1)
    expect(listDistributionReconciliationIssues).toHaveBeenCalledTimes(2)
    expect(getDistributionReconciliationSummary).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('admin.distribution.messages.reconciliationCompleted')
  })

  it('repairs a reconciliation issue and loads its action history', async () => {
    mockRoute.path = '/admin/distribution/reconciliation'
    mockRoute.params = { tab: 'reconciliation' }
    const issue = {
      id: 501,
      fingerprint: 'wallet-total-consumed:88',
      issue_type: 'wallet_total_consumed_mismatch',
      severity: 'warning',
      status: 'open',
      channel_org_id: 88,
      organization_name: 'Independent Agent',
      billing_usage_entry_id: null,
      commission_id: null,
      expected_amount: 10,
      actual_amount: 5,
      details: { transaction_count: 1 },
      occurrence_count: 1,
      repairable: true,
      last_detected_run_id: 42,
      resolution_action: '',
      resolution_note: '',
      resolved_by_user_id: null,
      first_detected_at: '2026-05-24T00:00:00Z',
      last_detected_at: '2026-05-24T00:00:00Z',
      resolved_at: null,
      created_at: '2026-05-24T00:00:00Z',
      updated_at: '2026-05-24T00:00:00Z',
    }
    listDistributionReconciliationIssues.mockResolvedValue({
      items: [issue],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    resolveDistributionReconciliationIssue.mockResolvedValue({ ...issue, status: 'resolved' })

    const wrapper = mount(AdminDistributionView, {
      global: {
        stubs: {
          AdminDistributionLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
          DataTable: DataTableStub,
          Pagination: true,
          BaseDialog: BaseDialogStub,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const handleButton = wrapper.findAll('button').find((button) => button.text() === 'admin.distribution.actions.handleReconciliationIssue')
    expect(handleButton).toBeDefined()
    await handleButton!.trigger('click')
    await flushPromises()

    expect(listDistributionReconciliationIssueActions).toHaveBeenCalledWith(501)
    const view = wrapper.vm as unknown as {
      reconciliationForm: { action: 'repair' | 'dismiss' | 'reopen' | 'recheck'; note: string }
    }
    view.reconciliationForm.action = 'repair'
    view.reconciliationForm.note = 'recalculate wallet aggregate'
    await wrapper.vm.$nextTick()
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(resolveDistributionReconciliationIssue).toHaveBeenCalledWith(501, {
      action: 'repair',
      note: 'recalculate wallet aggregate',
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.distribution.messages.reconciliationIssueUpdated')
    expect(listDistributionReconciliationIssues).toHaveBeenCalledTimes(2)
  })
})
