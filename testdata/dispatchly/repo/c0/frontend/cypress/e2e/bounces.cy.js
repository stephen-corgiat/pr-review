// Checks the bounce settings form shows the enabled webhook providers.
describe('Bounce settings', () => {
  it('lists the webhook providers once webhooks are on', () => {
    cy.visit('/admin/settings#bounces');
    cy.contains('Enable bounce webhooks').click();
    cy.contains('Enable Sendgrid').should('be.visible');
    cy.contains('Enable Postmark').should('be.visible');
  });
});
